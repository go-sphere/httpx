package fiberx

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-sphere/httpx"
	"github.com/gofiber/fiber/v3"
)

// A native layer on fiber always sits outside the middleware registered with
// Use, because Use composes into the route while UseNative occupies a real fiber
// stack entry ahead of it — including a layer registered after it.
func TestUseNativeRunsOutsideUse(t *testing.T) {
	var marks []string
	mark := func(s string) { marks = append(marks, s) }
	middleware := func(name string) httpx.Middleware {
		return func(next httpx.Handler) httpx.Handler {
			return func(ctx httpx.Context) error {
				mark(name + "-pre")
				err := next(ctx)
				mark(name + "-post")
				return err
			}
		}
	}

	app := New()
	r := app.Group("")
	r.Use(middleware("a"))
	router, ok := r.(*Router)
	if !ok {
		t.Fatalf("Group returned %T, want *Router", r)
	}
	router.UseNative(func(c fiber.Ctx) error {
		mark("native-pre")
		err := c.Next()
		mark("native-post")
		return err
	})
	r.Use(middleware("b"))
	r.GET("/native", func(ctx httpx.Context) error {
		mark("handler")
		return ctx.NoContent(http.StatusNoContent)
	})

	requester, ok := httpx.AsTestRequester(app)
	if !ok {
		t.Fatal("engine is not a TestRequester")
	}
	resp, err := requester.Do(httptest.NewRequest(http.MethodGet, "/native", nil))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	want := "native-pre,a-pre,b-pre,handler,b-post,a-post,native-post"
	if got := strings.Join(marks, ","); got != want {
		t.Fatalf("order mismatch\n got: %s\nwant: %s", got, want)
	}
}

func TestEngineUseNative(t *testing.T) {
	var ran bool
	app := New()
	engine, ok := app.(*Engine)
	if !ok {
		t.Fatalf("New returned %T, want *Engine", app)
	}
	engine.UseNative(func(c fiber.Ctx) error {
		ran = true
		return c.Next()
	})
	app.Group("").GET("/x", func(ctx httpx.Context) error {
		return ctx.NoContent(http.StatusNoContent)
	})

	requester, ok := httpx.AsTestRequester(app)
	if !ok {
		t.Fatal("engine is not a TestRequester")
	}
	if _, err := requester.Do(httptest.NewRequest(http.MethodGet, "/x", nil)); err != nil {
		t.Fatal(err)
	}
	if !ran {
		t.Fatal("native engine middleware did not run")
	}
}

// FromFiber takes the fiber.Ctx interface and hides the generic context type;
// both instantiations must come back as a working httpx.Context.
func TestFromFiber(t *testing.T) {
	app := New()
	engine, ok := app.(*Engine)
	if !ok {
		t.Fatalf("New returned %T, want *Engine", app)
	}
	// Registered before the route: fiber matches its stack in registration
	// order, so a native layer added afterwards would never be reached.
	engine.UseNative(func(c fiber.Ctx) error {
		ctx := FromFiber(c)
		if ctx.Method() != http.MethodGet {
			t.Errorf("Method = %q", ctx.Method())
		}
		if ctx.Query("a") != "1" {
			t.Errorf("Query(a) = %q", ctx.Query("a"))
		}
		return ctx.Text(http.StatusTeapot, "from-fiber")
	})
	app.Group("").GET("/from", func(httpx.Context) error { return nil })

	requester, ok := httpx.AsTestRequester(app)
	if !ok {
		t.Fatal("engine is not a TestRequester")
	}
	resp, err := requester.Do(httptest.NewRequest(http.MethodGet, "/from?a=1", nil))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusTeapot {
		t.Fatalf("status = %d, want 418", resp.StatusCode)
	}
}

// The named-wildcard mapping is recorded on the request by the route this
// adapter registered, so FromFiber resolves it on an adapter route and degrades
// — without panicking — on one registered natively, the way FromStd degrades for
// having no Engine.
func TestFromFiberNamedWildcardScope(t *testing.T) {
	app := New()
	engine, ok := app.(*Engine)
	if !ok {
		t.Fatalf("New returned %T, want *Engine", app)
	}
	// fiber pools its contexts, so every reading is taken inside the handler
	// rather than by holding the context past the request.
	type reading struct{ fullPath, named, anonymous string }
	var adapterRoute, nativeRoute reading
	read := func(ctx httpx.Context) reading {
		return reading{ctx.FullPath(), ctx.Param("filepath"), ctx.Param("*")}
	}

	// A route the adapter registered: the mapping is on the request, so a
	// context built from the native one mid-chain resolves the name.
	app.Group("").GET("/files/*filepath", func(ctx httpx.Context) error {
		// app.Test serves on its own goroutine, so a failure is recorded here
		// and asserted after the request.
		fc, ok := httpx.AsNativeContext[fiber.Ctx](ctx)
		if !ok {
			t.Error("native fiber context unavailable")
			return ctx.NoContent(http.StatusNoContent)
		}
		adapterRoute = read(FromFiber(fc))
		return ctx.NoContent(http.StatusNoContent)
	})
	// A route registered straight on fiber, bypassing the adapter's normalization.
	engine.engine.Get("/native/*", func(c fiber.Ctx) error {
		nativeRoute = read(FromFiber(c))
		return c.SendStatus(http.StatusNoContent)
	})

	requester, ok := httpx.AsTestRequester(app)
	if !ok {
		t.Fatal("engine is not a TestRequester")
	}
	for _, target := range []string{"/files/a/b.txt", "/native/a/b.txt"} {
		resp, err := requester.Do(httptest.NewRequest(http.MethodGet, target, nil))
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusNoContent {
			t.Fatalf("GET %s: status = %d, want 204", target, resp.StatusCode)
		}
	}

	if got := adapterRoute.fullPath; got != "/files/*filepath" {
		t.Fatalf("FromFiber FullPath on an adapter route = %q, want %q", got, "/files/*filepath")
	}
	if got := adapterRoute.named; got != "a/b.txt" {
		t.Fatalf("FromFiber Param(filepath) on an adapter route = %q, want %q", got, "a/b.txt")
	}
	if got := nativeRoute.fullPath; got != "/native/*" {
		t.Fatalf("FromFiber FullPath on a native route = %q, want fiber's own %q", got, "/native/*")
	}
	if got := nativeRoute.named; got != "" {
		t.Fatalf("FromFiber Param(filepath) on a native route = %q, want empty", got)
	}
	// The value is still reachable under the name fiber knows.
	if got := nativeRoute.anonymous; got != "a/b.txt" {
		t.Fatalf("FromFiber Param(*) on a native route = %q, want %q", got, "a/b.txt")
	}
}

// DefaultErrorHandler is fiber's native shape: the naming rule says it is the
// value the adapter installs on the framework, so it must stay assignable to
// fiber.Config.ErrorHandler, where NewConfig puts it.
func TestDefaultErrorHandlerIsNativeShape(t *testing.T) {
	cfg := fiber.Config{ErrorHandler: DefaultErrorHandler}
	if cfg.ErrorHandler == nil {
		t.Fatal("DefaultErrorHandler is not usable as fiber.Config.ErrorHandler")
	}
}
