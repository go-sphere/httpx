package ginx

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/go-sphere/httpx"
)

// These tests cover how ginx registers middleware on gin: what a middleware
// added after a route reaches, where native middleware sits relative to httpx
// middleware, and what Next does at the edges. Behavior shared by every
// adapter lives in the conformance suite; the composed form is in
// interceptor_test.go.

type chainRecorder struct{ marks []string }

func (r *chainRecorder) mark(s string) { r.marks = append(r.marks, s) }

func (r *chainRecorder) order() string { return strings.Join(r.marks, ",") }

func (r *chainRecorder) middleware(name string) httpx.Middleware {
	return func(ctx httpx.Context) error {
		r.mark(name + "-pre")
		err := ctx.Next()
		r.mark(name + "-post")
		return err
	}
}

func (r *chainRecorder) native(name string) gin.HandlerFunc {
	return func(c *gin.Context) {
		r.mark(name + "-pre")
		c.Next()
		r.mark(name + "-post")
	}
}

func (r *chainRecorder) handler(name string) httpx.Handler {
	return func(ctx httpx.Context) error {
		r.mark(name)
		return ctx.NoContent(http.StatusNoContent)
	}
}

func serveChain(t *testing.T, ge *gin.Engine, path string) *httptest.ResponseRecorder {
	t.Helper()
	rr := httptest.NewRecorder()
	ge.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, path, nil))
	return rr
}

func newChainEngine(t *testing.T, opts ...Option) (*gin.Engine, httpx.Engine) {
	t.Helper()
	gin.SetMode(gin.ReleaseMode)
	ge := gin.New()
	return ge, New(append([]Option{WithEngine(ge)}, opts...)...)
}

// A middleware added after a route was registered must not reach that route:
// gin binds each route to the handler chain that existed when it was
// registered.
func TestUseAfterRouteRegistration(t *testing.T) {
	rec := &chainRecorder{}
	ge, app := newChainEngine(t)
	r := app.Group("")
	r.Use(rec.middleware("first"))
	r.GET("/early", rec.handler("early"))
	r.Use(rec.middleware("second"))
	r.GET("/late", rec.handler("late"))

	if rr := serveChain(t, ge, "/early"); rr.Code != http.StatusNoContent {
		t.Fatalf("early status = %d", rr.Code)
	}
	if got := rec.order(); got != "first-pre,early,first-post" {
		t.Fatalf("early order = %s, want first-pre,early,first-post", got)
	}

	rec.marks = nil
	if rr := serveChain(t, ge, "/late"); rr.Code != http.StatusNoContent {
		t.Fatalf("late status = %d", rr.Code)
	}
	if got := rec.order(); got != "first-pre,second-pre,late,second-post,first-post" {
		t.Fatalf("late order = %s", got)
	}
}

// Native middleware registered straight on the gin engine sits between two
// httpx middlewares and must keep that position.
func TestNativeMiddlewareKeepsPosition(t *testing.T) {
	rec := &chainRecorder{}
	ge, app := newChainEngine(t)
	app.Use(rec.middleware("a"))
	ge.Use(rec.native("native"))
	app.Use(rec.middleware("b"))
	app.Group("").GET("/mixed", rec.handler("handler"))

	if rr := serveChain(t, ge, "/mixed"); rr.Code != http.StatusNoContent {
		t.Fatalf("status = %d", rr.Code)
	}
	want := "a-pre,native-pre,b-pre,handler,b-post,native-post,a-post"
	if got := rec.order(); got != want {
		t.Fatalf("order mismatch\n got: %s\nwant: %s", got, want)
	}
}

func TestUseNativeKeepsPosition(t *testing.T) {
	rec := &chainRecorder{}
	ge, app := newChainEngine(t)
	r := app.Group("")
	r.Use(rec.middleware("a"))
	router, ok := r.(*Router)
	if !ok {
		t.Fatalf("Group returned %T, want *Router", r)
	}
	router.UseNative(rec.native("native"))
	r.Use(rec.middleware("b"))
	r.GET("/native", rec.handler("handler"))

	if rr := serveChain(t, ge, "/native"); rr.Code != http.StatusNoContent {
		t.Fatalf("status = %d", rr.Code)
	}
	want := "a-pre,native-pre,b-pre,handler,b-post,native-post,a-post"
	if got := rec.order(); got != want {
		t.Fatalf("order mismatch\n got: %s\nwant: %s", got, want)
	}
}

// A second Next from the same middleware must not replay the layers after it.
func TestSecondNextIsNoop(t *testing.T) {
	rec := &chainRecorder{}
	ge, app := newChainEngine(t)
	r := app.Group("")
	r.Use(
		rec.middleware("a"),
		func(ctx httpx.Context) error {
			rec.mark("twice-pre")
			if err := ctx.Next(); err != nil {
				return err
			}
			if err := ctx.Next(); err != nil {
				return err
			}
			rec.mark("twice-post")
			return nil
		},
		rec.middleware("b"),
	)
	r.GET("/twice", rec.handler("handler"))

	if rr := serveChain(t, ge, "/twice"); rr.Code != http.StatusNoContent {
		t.Fatalf("status = %d", rr.Code)
	}
	want := "a-pre,twice-pre,b-pre,handler,b-post,twice-post,a-post"
	if got := rec.order(); got != want {
		t.Fatalf("order mismatch\n got: %s\nwant: %s", got, want)
	}
}

// A middleware that declines to continue stops the chain, and a Next from an
// outer layer afterwards must not restart it.
func TestMiddlewareWithoutNextStopsChain(t *testing.T) {
	rec := &chainRecorder{}
	ge, app := newChainEngine(t)
	r := app.Group("")
	r.Use(
		func(ctx httpx.Context) error {
			rec.mark("outer-pre")
			err := ctx.Next()
			rec.mark("outer-resumed")
			// The run is aborted; driving it again must not reach the layers
			// after the middleware that stopped it.
			if err2 := ctx.Next(); err2 != nil {
				return err2
			}
			rec.mark("outer-post")
			return err
		},
		func(ctx httpx.Context) error {
			rec.mark("stop")
			return ctx.Text(http.StatusForbidden, "stopped")
		},
		rec.middleware("after"),
	)
	r.GET("/abort", rec.handler("handler"))

	rr := serveChain(t, ge, "/abort")
	if rr.Code != http.StatusForbidden || strings.TrimSpace(rr.Body.String()) != "stopped" {
		t.Fatalf("response = %d %q", rr.Code, rr.Body.String())
	}
	want := "outer-pre,stop,outer-resumed,outer-post"
	if got := rec.order(); got != want {
		t.Fatalf("order mismatch\n got: %s\nwant: %s", got, want)
	}
}

// An error a layer records on the gin context, rather than returning it, is
// still reported to the layers outside that one.
func TestNativeErrorReachesOuterLayer(t *testing.T) {
	sentinel := errors.New("recorded downstream")
	var outerErr error
	ge, app := newChainEngine(t)
	r := app.Group("")
	r.Use(
		func(ctx httpx.Context) error {
			outerErr = ctx.Next()
			return nil
		},
		func(ctx httpx.Context) error {
			gc, ok := httpx.AsNativeContext[*gin.Context](ctx)
			if !ok {
				t.Fatal("native context unavailable")
			}
			_ = gc.Error(sentinel)
			return ctx.Next()
		},
	)
	r.GET("/recorded", func(ctx httpx.Context) error { return ctx.NoContent(http.StatusNoContent) })

	if rr := serveChain(t, ge, "/recorded"); rr.Code != http.StatusNoContent {
		t.Fatalf("status = %d", rr.Code)
	}
	if !errors.Is(outerErr, sentinel) {
		t.Fatalf("outer middleware got %v, want %v", outerErr, sentinel)
	}
}

type discardWriter struct {
	header http.Header
	status int
}

func (w *discardWriter) Header() http.Header         { return w.header }
func (w *discardWriter) WriteHeader(status int)      { w.status = status }
func (w *discardWriter) Write(p []byte) (int, error) { return len(p), nil }

// Engine-level middleware must also run for unmatched routes: gin rebuilds its
// no-route and no-method chains from the engine handlers, so Engine.Use has to
// go through gin's own Use.
func TestEngineMiddlewareReachesNoRoute(t *testing.T) {
	rec := &chainRecorder{}
	ge, app := newChainEngine(t)
	app.Use(rec.middleware("a"))
	app.Use(rec.middleware("b"))
	app.Group("").GET("/known", rec.handler("handler"))

	if rr := serveChain(t, ge, "/unknown"); rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusNotFound)
	}
	if got := rec.order(); got != "a-pre,b-pre,b-post,a-post" {
		t.Fatalf("no-route order = %s, want a-pre,b-pre,b-post,a-post", got)
	}
}
