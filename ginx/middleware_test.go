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

// These pin ginx-specific middleware placement: httpx layers compose into each
// route rather than taking a gin handler slot, which decides the order, what a
// late Use reaches, and where native gin middleware sits. Shared behavior lives
// in the conformance suite.

type chainRecorder struct{ marks []string }

func (r *chainRecorder) mark(s string) { r.marks = append(r.marks, s) }

func (r *chainRecorder) order() string { return strings.Join(r.marks, ",") }

func (r *chainRecorder) middleware(name string) httpx.Middleware {
	return func(next httpx.Handler) httpx.Handler {
		return func(ctx httpx.Context) error {
			r.mark(name + "-pre")
			err := next(ctx)
			r.mark(name + "-post")
			return err
		}
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

// Layers run in registration order, parent scopes first, and a group inherits
// its parents' chain by reference rather than by copy.
func TestMiddlewareOrderAndInheritance(t *testing.T) {
	rec := &chainRecorder{}
	ge, app := newChainEngine(t)
	app.Use(rec.middleware("root"))
	mid := app.Group("/nested", rec.middleware("mid"))
	leaf := mid.Group("/deep")
	leaf.Use(rec.middleware("leaf"))
	leaf.GET("/route", rec.handler("handler"))

	if rr := serveChain(t, ge, "/nested/deep/route"); rr.Code != http.StatusNoContent {
		t.Fatalf("status = %d", rr.Code)
	}
	want := "root-pre,mid-pre,leaf-pre,handler,leaf-post,mid-post,root-post"
	if got := rec.order(); got != want {
		t.Fatalf("order mismatch\n got: %s\nwant: %s", got, want)
	}
}

// A sibling group must not see middleware registered on another group, and a
// route registered before a Use call must not see it either: a registered route
// keeps the chain it was registered with, the rule gin applies to its own Use.
func TestMiddlewareScopeIsolation(t *testing.T) {
	rec := &chainRecorder{}
	ge, app := newChainEngine(t)
	r := app.Group("")
	r.Use(rec.middleware("first"))
	r.GET("/early", rec.handler("early"))
	r.Use(rec.middleware("second"))
	r.GET("/late", rec.handler("late"))
	sibling := app.Group("/sibling")
	sibling.GET("/route", rec.handler("sibling"))

	for _, tc := range []struct{ path, want string }{
		{"/early", "first-pre,early,first-post"},
		{"/late", "first-pre,second-pre,late,second-post,first-post"},
		{"/sibling/route", "sibling"},
	} {
		rec.marks = nil
		if rr := serveChain(t, ge, tc.path); rr.Code != http.StatusNoContent {
			t.Fatalf("%s: status = %d", tc.path, rr.Code)
		}
		if got := rec.order(); got != tc.want {
			t.Fatalf("%s order = %s, want %s", tc.path, got, tc.want)
		}
	}
}

// Returning without calling next is how a layer stops the chain.
func TestMiddlewareStopsChain(t *testing.T) {
	rec := &chainRecorder{}
	ge, app := newChainEngine(t)
	r := app.Group("")
	r.Use(
		rec.middleware("outer"),
		func(next httpx.Handler) httpx.Handler {
			return func(ctx httpx.Context) error {
				rec.mark("stop")
				return ctx.Text(http.StatusForbidden, "stopped")
			}
		},
		rec.middleware("inner"),
	)
	r.GET("/stop", rec.handler("handler"))

	rr := serveChain(t, ge, "/stop")
	if rr.Code != http.StatusForbidden || rr.Body.String() != "stopped" {
		t.Fatalf("response = %d %q", rr.Code, rr.Body.String())
	}
	if got := rec.order(); got != "outer-pre,stop,outer-post" {
		t.Fatalf("order = %s, want outer-pre,stop,outer-post", got)
	}
}

// An error from an inner layer reaches the outer layers as the return value of
// next and is rendered once, at the route where the chain was composed.
func TestMiddlewareErrorReachesOuterLayer(t *testing.T) {
	sentinel := httpx.NewUnauthorizedError("denied")
	var outerErr error
	var outerStatus int
	rec := &chainRecorder{}
	ge, app := newChainEngine(t)
	r := app.Group("")
	r.Use(
		func(next httpx.Handler) httpx.Handler {
			return func(ctx httpx.Context) error {
				err := next(ctx)
				outerErr, outerStatus = err, ctx.StatusCode()
				return err
			}
		},
		func(next httpx.Handler) httpx.Handler {
			return func(ctx httpx.Context) error { return sentinel }
		},
	)
	r.GET("/fail", rec.handler("handler"))

	rr := serveChain(t, ge, "/fail")
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusUnauthorized)
	}
	if rec.order() != "" {
		t.Fatalf("handler ran: %s", rec.order())
	}
	if !errors.Is(outerErr, sentinel) {
		t.Fatalf("outer layer got %v, want %v", outerErr, sentinel)
	}
	// Rendering happens at the route, so the status is not yet set while the
	// outer layers unwind — which is why a layer that measures the outcome has
	// to read the error next returned.
	if outerStatus == http.StatusUnauthorized {
		t.Fatal("status was already rendered inside the composed chain; update the documented contract")
	}
}

// Native gin middleware keeps its own handler slot, so it always wraps the
// composed chain — including a layer registered after it.
func TestNativeMiddlewareWrapsComposedChain(t *testing.T) {
	rec := &chainRecorder{}
	ge, app := newChainEngine(t)
	app.Use(rec.middleware("a"))
	ge.Use(rec.native("native"))
	app.Use(rec.middleware("b"))
	app.Group("").GET("/mixed", rec.handler("handler"))

	if rr := serveChain(t, ge, "/mixed"); rr.Code != http.StatusNoContent {
		t.Fatalf("status = %d", rr.Code)
	}
	want := "native-pre,a-pre,b-pre,handler,b-post,a-post,native-post"
	if got := rec.order(); got != want {
		t.Fatalf("order mismatch\n got: %s\nwant: %s", got, want)
	}
}

func TestUseNativeWrapsComposedChain(t *testing.T) {
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
	want := "native-pre,a-pre,b-pre,handler,b-post,a-post,native-post"
	if got := rec.order(); got != want {
		t.Fatalf("order mismatch\n got: %s\nwant: %s", got, want)
	}
}

type discardWriter struct {
	header http.Header
	status int
}

func (w *discardWriter) Header() http.Header         { return w.header }
func (w *discardWriter) WriteHeader(status int)      { w.status = status }
func (w *discardWriter) Write(p []byte) (int, error) { return len(p), nil }

// Engine-level middleware must also run for unmatched routes, which an access
// log or a recovery layer depends on: gin rebuilds its no-route chain from the
// engine handlers, and the composed chain reaches it through the NoRoute
// fallback.
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

// Composing into the route must not disturb the native chain: routes, 404
// handling and UseNative layers keep working around it, and the engine chain
// sits in the same place on an unmatched path as on a matched route.
func TestComposedChainLeavesNativeChainIntact(t *testing.T) {
	rec := &chainRecorder{}
	ge, app := newChainEngine(t)
	app.Use(rec.middleware("wrap"))
	app.(*Engine).UseNative(rec.native("native"))
	app.Group("").GET("/known", rec.handler("handler"))

	if rr := serveChain(t, ge, "/unknown"); rr.Code != http.StatusNotFound {
		t.Fatalf("no-route status = %d", rr.Code)
	}
	if got := rec.order(); got != "native-pre,wrap-pre,wrap-post,native-post" {
		t.Fatalf("no-route order = %s", got)
	}

	rec.marks = nil
	if rr := serveChain(t, ge, "/known"); rr.Code != http.StatusNoContent {
		t.Fatalf("status = %d", rr.Code)
	}
	want := "native-pre,wrap-pre,handler,wrap-post,native-post"
	if got := rec.order(); got != want {
		t.Fatalf("order mismatch\n got: %s\nwant: %s", got, want)
	}
}
