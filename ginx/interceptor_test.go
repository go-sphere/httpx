package ginx

import (
	"errors"
	"net/http"
	"testing"

	"github.com/go-sphere/httpx"
)

// Composed middleware (httpx.Interceptor) is registered by composing it
// into each route, so these tests pin the order that follows from that and the
// behavior a middleware author depends on: stopping the chain, error
// propagation, group inheritance, and interop with Next-driven middleware.

func (r *chainRecorder) interceptor(name string) httpx.Interceptor {
	return func(next httpx.Handler) httpx.Handler {
		return func(ctx httpx.Context) error {
			r.mark(name + "-pre")
			err := next(ctx)
			r.mark(name + "-post")
			return err
		}
	}
}

func TestInterceptorOrderAndInheritance(t *testing.T) {
	rec := &chainRecorder{}
	ge, app := newChainEngine(t)
	app.(*Engine).UseInterceptor(rec.interceptor("root"))
	mid := app.Group("/nested", rec.middleware("next-style"))
	mid.(*Router).UseInterceptor(rec.interceptor("mid"))
	leaf := mid.Group("/deep")
	leaf.(*Router).UseInterceptor(rec.interceptor("leaf"))
	leaf.GET("/route", rec.handler("handler"))

	if rr := serveChain(t, ge, "/nested/deep/route"); rr.Code != http.StatusNoContent {
		t.Fatalf("status = %d", rr.Code)
	}
	// Next-driven middleware holds a gin slot, so it stays outside the composed
	// chain; composed layers follow registration order, parents first.
	want := "next-style-pre,root-pre,mid-pre,leaf-pre,handler,leaf-post,mid-post,root-post,next-style-post"
	if got := rec.order(); got != want {
		t.Fatalf("order mismatch\n got: %s\nwant: %s", got, want)
	}
}

// A sibling group must not see middleware registered on another group, and a
// route registered before UseInterceptor must not see it either.
func TestInterceptorScopeIsolation(t *testing.T) {
	rec := &chainRecorder{}
	ge, app := newChainEngine(t)
	r := app.Group("")
	r.(*Router).UseInterceptor(rec.interceptor("first"))
	r.GET("/early", rec.handler("early"))
	r.(*Router).UseInterceptor(rec.interceptor("second"))
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

// Returning without calling next is how a composed middleware stops the chain.
func TestInterceptorStopsChain(t *testing.T) {
	rec := &chainRecorder{}
	ge, app := newChainEngine(t)
	r := app.Group("")
	r.(*Router).UseInterceptor(
		rec.interceptor("outer"),
		func(next httpx.Handler) httpx.Handler {
			return func(ctx httpx.Context) error {
				rec.mark("stop")
				return ctx.Text(http.StatusForbidden, "stopped")
			}
		},
		rec.interceptor("inner"),
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
func TestInterceptorErrorReachesOuterLayer(t *testing.T) {
	sentinel := httpx.NewUnauthorizedError("denied")
	var outerErr error
	var outerStatus int
	rec := &chainRecorder{}
	ge, app := newChainEngine(t)
	r := app.Group("")
	r.(*Router).UseInterceptor(
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
	// Documented difference from Use: rendering happens at the route, so the
	// status is not yet set while the outer layers unwind.
	if outerStatus == http.StatusUnauthorized {
		t.Fatal("status was already rendered inside the composed chain; update the documented contract")
	}
}

// Next-driven middleware can run inside a composed chain, and composed
// middleware can be registered through Use.
func TestInterceptorInteropBothDirections(t *testing.T) {
	rec := &chainRecorder{}
	ge, app := newChainEngine(t)
	r := app.Group("")
	r.Use(httpx.AsMiddleware(rec.interceptor("wrap-via-use")))
	r.(*Router).UseInterceptor(
		rec.interceptor("wrap"),
		httpx.AsInterceptor(rec.middleware("next-via-wrap")),
		rec.interceptor("inner"),
	)
	r.GET("/interop", rec.handler("handler"))

	if rr := serveChain(t, ge, "/interop"); rr.Code != http.StatusNoContent {
		t.Fatalf("status = %d", rr.Code)
	}
	want := "wrap-via-use-pre,wrap-pre,next-via-wrap-pre,inner-pre,handler," +
		"inner-post,next-via-wrap-post,wrap-post,wrap-via-use-post"
	if got := rec.order(); got != want {
		t.Fatalf("order mismatch\n got: %s\nwant: %s", got, want)
	}
}

// UseInterceptor must not disturb the native chain: routes, 404 handling and
// Next-driven middleware keep working around it.
func TestInterceptorLeavesNativeChainIntact(t *testing.T) {
	rec := &chainRecorder{}
	ge, app := newChainEngine(t)
	app.(*Engine).UseInterceptor(rec.interceptor("wrap"))
	app.Use(rec.middleware("next"))
	app.(*Engine).UseNative(rec.native("native"))
	app.Group("").GET("/known", rec.handler("handler"))

	if rr := serveChain(t, ge, "/unknown"); rr.Code != http.StatusNotFound {
		t.Fatalf("no-route status = %d", rr.Code)
	}
	if got := rec.order(); got != "next-pre,native-pre,native-post,next-post" {
		t.Fatalf("no-route order = %s", got)
	}

	rec.marks = nil
	if rr := serveChain(t, ge, "/known"); rr.Code != http.StatusNoContent {
		t.Fatalf("status = %d", rr.Code)
	}
	want := "next-pre,native-pre,wrap-pre,handler,wrap-post,native-post,next-post"
	if got := rec.order(); got != want {
		t.Fatalf("order mismatch\n got: %s\nwant: %s", got, want)
	}
}
