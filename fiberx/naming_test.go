package fiberx

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-sphere/httpx"
	"github.com/gofiber/fiber/v3"
)

// These tests cover the unified option surface: UseNative takes fiber.Handler,
// and FromFiber builds an httpx.Context from a native fiber.Ctx.

// Unlike gin/echo/hertz, a native layer on fiber always sits outside the
// middleware registered with Use, because Use composes into the route while
// UseNative occupies a real fiber stack entry ahead of it. Pinning the order
// here keeps that documented deviation honest.
func TestUseNativeRunsOutsideUse(t *testing.T) {
	var marks []string
	mark := func(s string) { marks = append(marks, s) }
	middleware := func(name string) httpx.Middleware {
		return func(ctx httpx.Context) error {
			mark(name + "-pre")
			err := ctx.Next()
			mark(name + "-post")
			return err
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

// FromFiber takes the fiber.Ctx interface and hides the generic context type.
// Both instantiations must come back as a working httpx.Context.
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

// DefaultErrorHandler is fiber's native shape: the naming rule says it is the
// value the adapter installs on the framework, so it must stay assignable to
// the fiber.Config field NewConfig puts it in.
func TestDefaultErrorHandlerIsNativeShape(t *testing.T) {
	cfg := fiber.Config{ErrorHandler: DefaultErrorHandler}
	if cfg.ErrorHandler == nil {
		t.Fatal("DefaultErrorHandler is not usable as fiber.Config.ErrorHandler")
	}
}
