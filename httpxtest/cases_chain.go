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
// what has been recorded when it runs, so the golden pins the order on the way
// *in*; the case also states the full sequence inline, including unwinding,
// which a response body cannot carry and a regenerated golden cannot accept.
type orderRecorder struct{ marks []string }

func (o *orderRecorder) mark(s string) { o.marks = append(o.marks, s) }

func (o *orderRecorder) joined() string { return strings.Join(o.marks, ",") }

func (o *orderRecorder) middleware(name string) httpx.Middleware {
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

	// httpx middleware composes into the route rather than taking a slot in the
	// framework's chain, so it runs inside everything UseNative registered on the
	// same scope, including a layer registered *after* the native one.
	t.Run("NativeMiddlewareWrapsLayers", func(t *testing.T) {
		if r.suite.NativeMiddleware == nil {
			t.Skipf("%s: no NativeMiddleware hook declared", r.suite.Name)
		}
		rec := &orderRecorder{}
		want := "native-pre,a-pre,b-pre,handler,b-post,a-post,native-post"
		r.assertChain(t, rec, want, func(router httpx.Router) {
			router.Use(rec.middleware("a"))
			r.suite.NativeMiddleware(router, rec.mark)
			router.Use(rec.middleware("b"))
			router.GET(chainPath, rec.handler)
		}, chainPath)
	})

	// Enough layers, spread over nested groups and both registration forms
	// (Group's variadic and Use), that batching or reordering shows up here.
	t.Run("DeepChain", func(t *testing.T) {
		rec := &orderRecorder{}
		want := strings.Join([]string{
			"root1-pre", "root2-pre", "mid-arg-pre", "mid-use-pre",
			"leaf1-pre", "leaf2-pre", "handler",
			"leaf2-post", "leaf1-post",
			"mid-use-post", "mid-arg-post", "root2-post", "root1-post",
		}, ",")
		r.assertChain(t, rec, want, func(router httpx.Router) {
			router.Use(rec.middleware("root1"), rec.middleware("root2"))

			// Group's variadic is the one-line form of Group(prefix) followed by
			// Use, so it lands ahead of the later Use on the same scope: within a
			// scope, layers run in registration order.
			mid := router.Group("/mid", rec.middleware("mid-arg"))
			mid.Use(rec.middleware("mid-use"))

			leaf := mid.Group("/leaf")
			leaf.Use(rec.middleware("leaf1"), rec.middleware("leaf2"))
			leaf.GET(chainPath, rec.handler)
		}, "/mid/leaf"+chainPath)
	})

	t.Run("GroupInheritance", func(t *testing.T) {
		rec := &orderRecorder{}
		r.assertChain(t, rec, "root-pre,mid-pre,handler,mid-post,root-post", func(router httpx.Router) {
			router.Use(rec.middleware("root"))
			mid := router.Group("/mid")
			mid.Use(rec.middleware("mid"))
			mid.GET(chainPath, rec.handler)
			// A sibling registered afterwards must not inherit mid's layer.
			router.Group("/sibling").GET(chainPath, rec.handler)
		}, "/mid"+chainPath)
	})

	t.Run("SiblingGroupIsolation", func(t *testing.T) {
		rec := &orderRecorder{}
		r.assertChain(t, rec, "root-pre,handler,root-post", func(router httpx.Router) {
			router.Use(rec.middleware("root"))
			mid := router.Group("/mid")
			mid.Use(rec.middleware("mid"))
			mid.GET(chainPath, rec.handler)
			router.Group("/sibling").GET(chainPath, rec.handler)
		}, "/sibling"+chainPath)
	})

	// Returning without calling next is how a layer stops the chain: nothing
	// below it runs and the layers outside it still resume.
	t.Run("MiddlewareStopsChain", func(t *testing.T) {
		rec := &orderRecorder{}
		r.assertChain(t, rec, "a-pre,stop,a-post", func(router httpx.Router) {
			router.Use(
				rec.middleware("a"),
				func(next httpx.Handler) httpx.Handler {
					return func(ctx httpx.Context) error {
						rec.mark("stop")
						return ctx.Text(http.StatusForbidden, "stopped")
					}
				},
				rec.middleware("b"),
			)
			router.GET(chainPath, rec.handler)
		}, chainPath)
	})

	// Errors raised several layers down must reach the layer that called next,
	// with everything the layers between it added.
	t.Run("ErrorsJoinOnTheWayOut", func(t *testing.T) {
		errA := errors.New("err-a")
		errB := errors.New("err-b")
		var outerErr error

		got := r.serve(t, func(router httpx.Router) {
			router.Use(func(next httpx.Handler) httpx.Handler {
				return func(ctx httpx.Context) error {
					outerErr = next(ctx)
					return outerErr
				}
			})
			router.Use(func(next httpx.Handler) httpx.Handler {
				return func(ctx httpx.Context) error {
					err := next(ctx)
					if err == nil {
						return errB
					}
					return errors.Join(err, errB)
				}
			})
			router.GET(chainPath, func(ctx httpx.Context) error { return errA })
		}, httptest.NewRequest(http.MethodGet, "http://example.com"+chainPath, nil))

		if got.Status != http.StatusInternalServerError {
			t.Fatalf("status = %d, want %d", got.Status, http.StatusInternalServerError)
		}
		if !errors.Is(outerErr, errA) || !errors.Is(outerErr, errB) {
			t.Fatalf("outer next() = %v, want it to contain both errA and errB", outerErr)
		}
	})

	// An inner failure must not run the handler, must still let the layers above
	// it resume, and must reach them as an error value: the chain is rendered
	// where it was composed — at the route — not at the layer that failed, so a
	// layer logging the outcome must read what next returned, not the status.
	t.Run("ErrorFromInnerLayer", func(t *testing.T) {
		var outerErr error
		var outerResumed, handlerRan bool

		got := r.serve(t, func(router httpx.Router) {
			router.Use(
				func(next httpx.Handler) httpx.Handler {
					return func(ctx httpx.Context) error {
						err := next(ctx)
						outerResumed, outerErr = true, err
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
			t.Fatal("handler ran after the chain failed")
		}
		if !outerResumed {
			t.Fatal("the outer layer did not resume after the inner failure")
		}
		if outerErr == nil {
			t.Fatal("the outer layer got nil from next after the inner failure")
		}
	})
}
