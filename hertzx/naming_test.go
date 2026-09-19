package hertzx

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/test/mock"
	"github.com/go-sphere/httpx"
)

// Native hertz middleware keeps its own handler slot, so it wraps the whole
// composed chain — including a layer registered after it.
func TestUseNativeWrapsComposedChain(t *testing.T) {
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

	h := server.New(server.WithDisablePrintRoute(true))
	app0 := New(WithEngine(h))
	r := app0.Group("")
	r.Use(middleware("a"))
	router, ok := r.(*Router)
	if !ok {
		t.Fatalf("Group returned %T, want *Router", r)
	}
	router.UseNative(func(ctx context.Context, rc *app.RequestContext) {
		mark("native-pre")
		rc.Next(ctx)
		mark("native-post")
	})
	r.Use(middleware("b"))
	r.GET("/native", func(ctx httpx.Context) error {
		mark("handler")
		return ctx.NoContent(http.StatusNoContent)
	})

	rc := h.NewContext()
	rc.SetConn(mock.NewConn(""))
	rc.Request.SetRequestURI("http://example.com/native")
	h.ServeHTTP(t.Context(), rc)

	want := "native-pre,a-pre,b-pre,handler,b-post,a-post,native-post"
	if got := strings.Join(marks, ","); got != want {
		t.Fatalf("order mismatch\n got: %s\nwant: %s", got, want)
	}
}

func TestEngineUseNative(t *testing.T) {
	var ran bool
	h := server.New(server.WithDisablePrintRoute(true))
	app0 := New(WithEngine(h))
	engine, ok := app0.(*Engine)
	if !ok {
		t.Fatalf("New returned %T, want *Engine", app0)
	}
	engine.UseNative(func(ctx context.Context, rc *app.RequestContext) {
		ran = true
		rc.Next(ctx)
	})
	app0.Group("").GET("/x", func(ctx httpx.Context) error {
		return ctx.NoContent(http.StatusNoContent)
	})

	rc := h.NewContext()
	rc.SetConn(mock.NewConn(""))
	rc.Request.SetRequestURI("http://example.com/x")
	h.ServeHTTP(t.Context(), rc)
	if !ran {
		t.Fatal("native engine middleware did not run")
	}
}

// WithNativeErrorHandler keeps hertz's own handler shape; it aborts the chain
// the way the httpx-typed handler does.
func TestWithNativeErrorHandler(t *testing.T) {
	// DefaultErrorHandler is hertz's native shape, so it goes through the
	// native option — that assignability is what the naming rule promises.
	_ = WithNativeErrorHandler(DefaultErrorHandler)

	h := server.New(server.WithDisablePrintRoute(true))
	engine := New(WithEngine(h), WithNativeErrorHandler(func(ctx context.Context, rc *app.RequestContext, err error) {
		rc.JSON(http.StatusTeapot, map[string]string{"error": err.Error()})
		rc.Abort()
	}))
	engine.Group("").GET("/boom", func(ctx httpx.Context) error {
		return httpx.NewInternalServerError("boom")
	})

	rc := h.NewContext()
	rc.SetConn(mock.NewConn(""))
	rc.Request.SetRequestURI("http://example.com/boom")
	h.ServeHTTP(t.Context(), rc)
	if got := rc.Response.StatusCode(); got != http.StatusTeapot {
		t.Fatalf("status = %d, want 418", got)
	}
}
