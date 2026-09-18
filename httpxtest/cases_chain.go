package httpxtest

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-sphere/httpx"
)

func init() {
	register("Chain", casesChain)
}

// orderRecorder collects the execution order of a chain. The handler returns
// what has been recorded when it runs, so the golden contract pins the order
// every adapter must produce on the way *in*; the case also states the full
// sequence inline, including unwinding, which a response body cannot carry and
// which regenerating a golden therefore cannot quietly accept.
type orderRecorder struct{ marks []string }

func (o *orderRecorder) mark(s string) { o.marks = append(o.marks, s) }

func (o *orderRecorder) joined() string { return strings.Join(o.marks, ",") }

func (o *orderRecorder) middleware(name string) httpx.Middleware {
	return func(ctx httpx.Context) error {
		o.mark(name + "-pre")
		err := ctx.Next()
		o.mark(name + "-post")
		return err
	}
}

func (o *orderRecorder) interceptor(name string) httpx.Interceptor {
	return func(next httpx.Handler) httpx.Handler {
		return func(ctx httpx.Context) error {
			o.mark(name + "-pre")
			err := next(ctx)
			o.mark(name + "-post")
			return err
		}
	}
}

func (o *orderRecorder) handler(ctx httpx.Context) error {
	o.mark("handler")
	return ctx.JSON(http.StatusOK, map[string]any{"order": o.marks})
}

const chainPath = "/chain/order"

// assertChain serves path, checks the full recorded sequence against want, and
// compares the response with the golden contract.
func (r runner) assertChain(t *testing.T, rec *orderRecorder, want string, register func(httpx.Router), path string) {
	t.Helper()
	got := r.serve(t, register, httptest.NewRequest(http.MethodGet, "http://example.com"+path, nil))
	if joined := rec.joined(); joined != want {
		t.Fatalf("order mismatch\n got: %s\nwant: %s", joined, want)
	}
	r.compareGolden(t, got)
}

func casesChain(t *testing.T, r runner) {
	t.Run("MiddlewareAroundHandler", func(t *testing.T) {
		rec := &orderRecorder{}
		r.assertChain(t, rec, "a-pre,b-pre,handler,b-post,a-post", func(router httpx.Router) {
			router.Use(rec.middleware("a"), rec.middleware("b"))
			router.GET(chainPath, rec.handler)
		}, chainPath)
	})

	// Interceptors compose into the route, so they run inside everything
	// registered with Use on the same scope whatever the call order was.
	t.Run("InterceptorInsideMiddleware", func(t *testing.T) {
		rec := &orderRecorder{}
		r.assertChain(t, rec, "mw-pre,i1-pre,i2-pre,handler,i2-post,i1-post,mw-post", func(router httpx.Router) {
			httpx.UseInterceptor(router, rec.interceptor("i1"))
			router.Use(rec.middleware("mw"))
			httpx.UseInterceptor(router, rec.interceptor("i2"))
			router.GET(chainPath, rec.handler)
		}, chainPath)
	})

	// Converting between the two forms must not change what runs where.
	t.Run("MixedFormsInterop", func(t *testing.T) {
		rec := &orderRecorder{}
		want := "wrapped-mw-pre,i-pre,wrapped-i-pre,handler,wrapped-i-post,i-post,wrapped-mw-post"
		r.assertChain(t, rec, want, func(router httpx.Router) {
			router.Use(httpx.AsMiddleware(rec.interceptor("wrapped-mw")))
			httpx.UseInterceptor(router,
				rec.interceptor("i"),
				httpx.AsInterceptor(rec.middleware("wrapped-i")),
			)
			router.GET(chainPath, rec.handler)
		}, chainPath)
	})

	// A native middleware continues the chain with its own framework's Next,
	// which must still reach the httpx middleware registered after it.
	t.Run("NativeMiddlewareBetweenLayers", func(t *testing.T) {
		if r.suite.NativeMiddleware == nil {
			t.Skipf("%s: no NativeMiddleware hook declared", r.suite.Name)
		}
		rec := &orderRecorder{}
		want := "a-pre,native-pre,b-pre,handler,b-post,native-post,a-post"
		r.assertChain(t, rec, want, func(router httpx.Router) {
			router.Use(
				rec.middleware("a"),
				r.suite.NativeMiddleware(rec.mark),
				rec.middleware("b"),
			)
			router.GET(chainPath, rec.handler)
		}, chainPath)
	})

	// Native middleware outside, httpx middleware next, interceptors innermost:
	// the shape a service ends up with when it mixes all three.
	t.Run("NativeMiddlewareWithInterceptors", func(t *testing.T) {
		if r.suite.NativeMiddleware == nil {
			t.Skipf("%s: no NativeMiddleware hook declared", r.suite.Name)
		}
		rec := &orderRecorder{}
		want := "native-pre,mw-pre,i-pre,handler,i-post,mw-post,native-post"
		r.assertChain(t, rec, want, func(router httpx.Router) {
			router.Use(r.suite.NativeMiddleware(rec.mark), rec.middleware("mw"))
			httpx.UseInterceptor(router, rec.interceptor("i"))
			router.GET(chainPath, rec.handler)
		}, chainPath)
	})

	// Enough layers, spread over nested groups and both forms, that an adapter
	// which batches or reorders anything shows up here.
	t.Run("DeepMixedChain", func(t *testing.T) {
		rec := &orderRecorder{}
		want := strings.Join([]string{
			"root-mw-pre", "mid-mw-pre", "leaf-mw1-pre", "leaf-mw2-pre",
			"root-i-pre", "mid-i-pre", "leaf-i-pre", "handler",
			"leaf-i-post", "mid-i-post", "root-i-post",
			"leaf-mw2-post", "leaf-mw1-post", "mid-mw-post", "root-mw-post",
		}, ",")
		r.assertChain(t, rec, want, func(router httpx.Router) {
			router.Use(rec.middleware("root-mw"))
			httpx.UseInterceptor(router, rec.interceptor("root-i"))

			mid := router.Group("/mid", rec.middleware("mid-mw"))
			httpx.UseInterceptor(mid, rec.interceptor("mid-i"))

			leaf := mid.Group("/leaf", rec.middleware("leaf-mw1"), rec.middleware("leaf-mw2"))
			httpx.UseInterceptor(leaf, rec.interceptor("leaf-i"))
			leaf.GET(chainPath, rec.handler)
		}, "/mid/leaf"+chainPath)
	})

	t.Run("GroupInheritance", func(t *testing.T) {
		rec := &orderRecorder{}
		r.assertChain(t, rec, "root-pre,mid-pre,handler,mid-post,root-post", func(router httpx.Router) {
			router.Use(rec.middleware("root"))
			mid := router.Group("/mid", rec.middleware("mid"))
			mid.GET(chainPath, rec.handler)
			// A sibling registered afterwards must not inherit mid's layer.
			router.Group("/sibling").GET(chainPath, rec.handler)
		}, "/mid"+chainPath)
	})

	t.Run("SiblingGroupIsolation", func(t *testing.T) {
		rec := &orderRecorder{}
		r.assertChain(t, rec, "root-pre,handler,root-post", func(router httpx.Router) {
			router.Use(rec.middleware("root"))
			mid := router.Group("/mid", rec.middleware("mid"))
			mid.GET(chainPath, rec.handler)
			router.Group("/sibling").GET(chainPath, rec.handler)
		}, "/sibling"+chainPath)
	})

	// Neither form may reach the handler after a layer stops the chain, and the
	// layers outside it still resume.
	t.Run("MiddlewareStopsChain", func(t *testing.T) {
		rec := &orderRecorder{}
		r.assertChain(t, rec, "a-pre,stop,a-post", func(router httpx.Router) {
			router.Use(
				rec.middleware("a"),
				func(ctx httpx.Context) error {
					rec.mark("stop")
					return ctx.Text(http.StatusForbidden, "stopped")
				},
				rec.middleware("b"),
			)
			router.GET(chainPath, rec.handler)
		}, chainPath)
	})

	t.Run("InterceptorStopsChain", func(t *testing.T) {
		rec := &orderRecorder{}
		r.assertChain(t, rec, "outer-pre,stop,outer-post", func(router httpx.Router) {
			httpx.UseInterceptor(router,
				rec.interceptor("outer"),
				func(next httpx.Handler) httpx.Handler {
					return func(ctx httpx.Context) error {
						rec.mark("stop")
						return ctx.Text(http.StatusForbidden, "stopped")
					}
				},
				rec.interceptor("inner"),
			)
			router.GET(chainPath, rec.handler)
		}, chainPath)
	})

	// Errors raised several layers down must reach the layer that drove the
	// chain, however the adapter aggregates them.
	t.Run("NextReturnsJoinedErrors", func(t *testing.T) {
		errA := errors.New("err-a")
		errB := errors.New("err-b")
		var outerErr error

		got := r.serve(t, func(router httpx.Router) {
			router.Use(func(ctx httpx.Context) error {
				outerErr = ctx.Next()
				return outerErr
			})
			router.Use(func(ctx httpx.Context) error {
				err := ctx.Next()
				if err == nil {
					return errB
				}
				return errors.Join(err, errB)
			})
			router.GET(chainPath, func(ctx httpx.Context) error { return errA })
		}, httptest.NewRequest(http.MethodGet, "http://example.com"+chainPath, nil))

		if got.Status != http.StatusInternalServerError {
			t.Fatalf("status = %d, want %d", got.Status, http.StatusInternalServerError)
		}
		if !errors.Is(outerErr, errA) || !errors.Is(outerErr, errB) {
			t.Fatalf("outer ctx.Next() = %v, want it to contain both errA and errB", outerErr)
		}
	})

	// An inner failure must not run the handler, must still let outer layers
	// resume, and — where the adapter renders in place — must already be
	// written when they do. See Caps.RendersErrorAtFailingLayer.
	t.Run("ErrorFromInnerMiddleware", func(t *testing.T) {
		var outerStatus int
		var outerErr error
		var outerResumed, handlerRan bool

		got := r.serve(t, func(router httpx.Router) {
			router.Use(
				func(ctx httpx.Context) error {
					err := ctx.Next()
					outerResumed, outerErr, outerStatus = true, err, ctx.StatusCode()
					return err
				},
				func(ctx httpx.Context) error {
					return httpx.NewUnauthorizedError("denied")
				},
			)
			router.GET(chainPath, func(ctx httpx.Context) error {
				handlerRan = true
				return ctx.NoContent(http.StatusNoContent)
			})
		}, httptest.NewRequest(http.MethodGet, "http://example.com"+chainPath, nil))

		if got.Status != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d", got.Status, http.StatusUnauthorized)
		}
		if handlerRan {
			t.Fatal("handler ran after the chain failed")
		}
		if !outerResumed {
			t.Fatal("outer middleware did not resume after the inner failure")
		}
		if outerErr == nil {
			t.Fatal("outer middleware got nil from Next after the inner failure")
		}
		if r.suite.Caps.RendersErrorAtFailingLayer && outerStatus != http.StatusUnauthorized {
			t.Fatalf("outer middleware observed status %d after Next, want %d (Caps.RendersErrorAtFailingLayer is set)",
				outerStatus, http.StatusUnauthorized)
		}
	})

	// An interceptor error is rendered where the chain was composed — at the
	// route — so the layers above it see the error value, not the status.
	t.Run("ErrorFromInnerInterceptor", func(t *testing.T) {
		var outerErr error
		handlerRan := false

		got := r.serve(t, func(router httpx.Router) {
			httpx.UseInterceptor(router,
				func(next httpx.Handler) httpx.Handler {
					return func(ctx httpx.Context) error {
						err := next(ctx)
						outerErr = err
						return err
					}
				},
				func(next httpx.Handler) httpx.Handler {
					return func(ctx httpx.Context) error {
						return httpx.NewUnauthorizedError("denied")
					}
				},
			)
			router.GET(chainPath, func(ctx httpx.Context) error {
				handlerRan = true
				return ctx.NoContent(http.StatusNoContent)
			})
		}, httptest.NewRequest(http.MethodGet, "http://example.com"+chainPath, nil))

		if got.Status != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d", got.Status, http.StatusUnauthorized)
		}
		if handlerRan {
			t.Fatal("handler ran after the interceptor chain failed")
		}
		if outerErr == nil {
			t.Fatal("outer interceptor got nil from next after the inner failure")
		}
	})
}
