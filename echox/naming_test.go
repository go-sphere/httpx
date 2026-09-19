package echox

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-sphere/httpx"
	"github.com/labstack/echo/v4"
)

// These tests cover the unified option surface: WithErrorHandler takes
// httpx.ErrorHandler on every adapter, UseNative takes the framework's own
// middleware type, and FromEcho builds an httpx.Context from a native one.

func TestUseNativeKeepsPosition(t *testing.T) {
	var marks []string
	mark := func(s string) { marks = append(marks, s) }
	native := func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			mark("native-pre")
			err := next(c)
			mark("native-post")
			return err
		}
	}
	middleware := func(name string) httpx.Middleware {
		return func(ctx httpx.Context) error {
			mark(name + "-pre")
			err := ctx.Next()
			mark(name + "-post")
			return err
		}
	}

	e := echo.New()
	app := New(WithEngine(e))
	r := app.Group("")
	r.Use(middleware("a"))
	router, ok := r.(*Router)
	if !ok {
		t.Fatalf("Group returned %T, want *Router", r)
	}
	router.UseNative(native)
	r.Use(middleware("b"))
	r.GET("/native", func(ctx httpx.Context) error {
		mark("handler")
		return ctx.NoContent(http.StatusNoContent)
	})

	rr := httptest.NewRecorder()
	e.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/native", nil))
	if rr.Code != http.StatusNoContent {
		t.Fatalf("status = %d", rr.Code)
	}
	want := "a-pre,native-pre,b-pre,handler,b-post,native-post,a-post"
	if got := strings.Join(marks, ","); got != want {
		t.Fatalf("order mismatch\n got: %s\nwant: %s", got, want)
	}
}

func TestEngineUseNative(t *testing.T) {
	var ran bool
	e := echo.New()
	app := New(WithEngine(e))
	engine, ok := app.(*Engine)
	if !ok {
		t.Fatalf("New returned %T, want *Engine", app)
	}
	engine.UseNative(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			ran = true
			return next(c)
		}
	})
	app.Group("").GET("/x", func(ctx httpx.Context) error {
		return ctx.NoContent(http.StatusNoContent)
	})

	rr := httptest.NewRecorder()
	e.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/x", nil))
	if !ran {
		t.Fatal("native engine middleware did not run")
	}
}

// FromEcho must produce a context that writes through the same echo response
// an ordinary handler would use.
func TestFromEcho(t *testing.T) {
	e := echo.New()
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/from?a=1", nil)
	ec := e.NewContext(req, rr)

	ctx := FromEcho(ec)
	if ctx.Method() != http.MethodGet {
		t.Fatalf("Method = %q", ctx.Method())
	}
	if ctx.Query("a") != "1" {
		t.Fatalf("Query(a) = %q", ctx.Query("a"))
	}
	if err := ctx.Next(); err != nil {
		t.Fatalf("Next on a detached context = %v, want nil", err)
	}
	if err := ctx.Text(http.StatusTeapot, "from-echo"); err != nil {
		t.Fatalf("Text: %v", err)
	}
	if rr.Code != http.StatusTeapot || rr.Body.String() != "from-echo" {
		t.Fatalf("response = %d %q", rr.Code, rr.Body.String())
	}
}

// A context built by FromEcho has no engine behind it, so it cannot resolve
// this adapter's named-wildcard normalization — the same degradation FromStd
// documents for having no Engine. It must degrade, not panic: the name lives in
// the engine's table, and the only thing that would let a detached context
// reach one is a process-wide table, which is what scoping the mapping per
// engine exists to avoid.
func TestFromEchoDegradesNamedWildcard(t *testing.T) {
	e := echo.New()
	app := New(WithEngine(e))

	var detached httpx.Context
	app.Group("").GET("/files/*filepath", func(ctx httpx.Context) error {
		// The adapter's own context resolves the name.
		if got := ctx.FullPath(); got != "/files/*filepath" {
			t.Errorf("adapter context FullPath = %q, want %q", got, "/files/*filepath")
		}
		if got := ctx.Param("filepath"); got != "a/b.txt" {
			t.Errorf("adapter context Param(filepath) = %q, want %q", got, "a/b.txt")
		}
		ec, ok := httpx.AsNativeContext[echo.Context](ctx)
		if !ok {
			t.Fatal("native echo context unavailable")
		}
		detached = FromEcho(ec)
		return ctx.NoContent(http.StatusNoContent)
	})

	rr := httptest.NewRecorder()
	e.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/files/a/b.txt", nil))
	if rr.Code != http.StatusNoContent {
		t.Fatalf("status = %d", rr.Code)
	}
	if detached == nil {
		t.Fatal("handler did not run")
	}
	if got := detached.FullPath(); got != "/files/*" {
		t.Fatalf("FromEcho FullPath = %q, want echo's own %q", got, "/files/*")
	}
	if got := detached.Param("filepath"); got != "" {
		t.Fatalf("FromEcho Param(filepath) = %q, want empty", got)
	}
	// The value is still reachable under the name echo knows.
	if got := detached.Param("*"); got != "a/b.txt" {
		t.Fatalf("FromEcho Param(*) = %q, want %q", got, "a/b.txt")
	}
	if got := detached.Params()["*"]; got != "a/b.txt" {
		t.Fatalf("FromEcho Params()[*] = %q, want %q", got, "a/b.txt")
	}
}

// DefaultErrorHandler is echo's native shape (echo.HTTPErrorHandler) and must
// stay assignable to it: that is what NewConfig installs on the engine.
func TestDefaultErrorHandlerIsNativeShape(t *testing.T) {
	e := echo.New()
	// Assignable to echo's own handler slot — which is what NewConfig relies
	// on when it installs the default.
	e.HTTPErrorHandler = DefaultErrorHandler

	e = echo.New()
	_ = New(WithEngine(e))
	if e.HTTPErrorHandler == nil {
		t.Fatal("adapter did not install an error handler")
	}

	rr := httptest.NewRecorder()
	ec := e.NewContext(httptest.NewRequest(http.MethodGet, "/", nil), rr)
	e.HTTPErrorHandler(echo.NewHTTPError(http.StatusNotFound), ec)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (echo.HTTPError status must survive)", rr.Code)
	}
}
