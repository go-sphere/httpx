package httpxtest

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-sphere/httpx"
)

func init() {
	register("ContextBoundary", casesContextBoundary)
}

// The ContextBoundary cases pin where a request-scoped value has to survive a
// change in the *kind* of code running, not just the layer: from an httpx layer
// to the layers above it after next returns, and from an httpx layer into a
// mounted net/http handler or a net/http middleware adapted by
// AdaptStdMiddleware.
//
// Both bridges hand the other side a *http.Request, so a value travels only
// through the standard context: ctx.Set reaches the httpx chain and stops
// there, ctx.SetContext is what continues across. The two are asserted side by
// side, because the difference — invisible while everything is httpx — is
// exactly what decides whether a tracing or logging value reaches code that
// only ever sees a context.Context.
type (
	boundaryCtxKey     struct{}
	boundaryHandlerKey struct{}
)

const boundaryStoreKey = "boundary.store"

func casesContextBoundary(t *testing.T, r runner) {
	// A value set by a layer below is visible to a layer above once next
	// returns, including one set by the handler itself. The outer layer is the
	// only writer here so the observation can be the response body; a header
	// written after a handler committed would be dropped by the net/http
	// adapters and kept by the buffered ones.
	t.Run("ValueSetBelowIsVisibleAbove", func(t *testing.T) {
		const layerValue, handlerValue = "from-layer", "from-handler"
		var outerErr error
		resp := r.serve(t, func(router httpx.Router) {
			router.Use(func(next httpx.Handler) httpx.Handler {
				return func(ctx httpx.Context) error {
					ctx.SetContext(context.WithValue(ctx.Context(), boundaryCtxKey{}, layerValue))
					return next(ctx)
				}
			})
			router.Use(func(next httpx.Handler) httpx.Handler {
				return func(ctx httpx.Context) error {
					err := next(ctx)
					outerErr = err
					if err != nil {
						return err
					}
					layer, _ := ctx.Context().Value(boundaryCtxKey{}).(string)
					handler, _ := ctx.Context().Value(boundaryHandlerKey{}).(string)
					return ctx.JSON(http.StatusOK, map[string]string{"layer": layer, "handler": handler})
				}
			})
			router.GET("/boundary/unwind", func(ctx httpx.Context) error {
				ctx.SetContext(context.WithValue(ctx.Context(), boundaryHandlerKey{}, handlerValue))
				return nil
			})
		}, httptest.NewRequest(http.MethodGet, "http://example.com/boundary/unwind", nil))

		if outerErr != nil {
			t.Fatalf("the outer layer got %v from next, want nil", outerErr)
		}
		if resp.Status != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%q", resp.Status, resp.Body)
		}
		r.compareGolden(t, resp)
	})

	// The same layer writing into a handler that is plain net/http: the value
	// has to be on the request the mounted handler is given. The store value is
	// read there too and must be absent — the boundary is the standard context,
	// not the httpx context.
	t.Run("ValueReachesMountedStdHandler", func(t *testing.T) {
		resp := r.serve(t, func(router httpx.Router) {
			router.Use(func(next httpx.Handler) httpx.Handler {
				return func(ctx httpx.Context) error {
					ctx.Set(boundaryStoreKey, "store-only")
					ctx.SetContext(context.WithValue(ctx.Context(), boundaryCtxKey{}, "from-layer"))
					return next(ctx)
				}
			})
			if !httpx.MountStd(router, http.MethodGet, "/boundary/mounted", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				value, _ := req.Context().Value(boundaryCtxKey{}).(string)
				stored, _ := req.Context().Value(boundaryStoreKey).(string)
				// Declared rather than left to sniffing: the in-process
				// requester of a net/http adapter does not sniff, so an unset
				// Content-Type would be a harness difference, not an adapter one.
				w.Header().Set("Content-Type", "text/plain; charset=utf-8")
				_, _ = fmt.Fprintf(w, "context=%q store=%q", value, stored)
			})) {
				t.Skip("adapter does not implement httpx.StdHandlerMounter")
			}
		}, httptest.NewRequest(http.MethodGet, "http://example.com/boundary/mounted", nil))

		if resp.Status != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%q", resp.Status, resp.Body)
		}
		r.compareGolden(t, resp)
	})

	// The other bridge: a net/http middleware wrapped by the adapter's
	// AdaptStdMiddleware runs outside the httpx handler it wraps, so the value
	// the layer above set has to be on the request that middleware receives.
	// What the std middleware saw is published through X-Trace, the header the
	// golden contracts record.
	t.Run("ValueReachesStdMiddleware", func(t *testing.T) {
		if r.suite.StdMiddleware == nil {
			t.Skipf("%s: no StdMiddleware hook declared", r.suite.Name)
		}
		mw := func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				value, _ := req.Context().Value(boundaryCtxKey{}).(string)
				stored, _ := req.Context().Value(boundaryStoreKey).(string)
				w.Header().Set("X-Trace", fmt.Sprintf("context=%q store=%q", value, stored))
				next.ServeHTTP(w, req)
			})
		}
		resp := r.serve(t, func(router httpx.Router) {
			router.Use(func(next httpx.Handler) httpx.Handler {
				return func(ctx httpx.Context) error {
					ctx.Set(boundaryStoreKey, "store-only")
					ctx.SetContext(context.WithValue(ctx.Context(), boundaryCtxKey{}, "from-layer"))
					return next(ctx)
				}
			}, r.suite.StdMiddleware(mw))
			router.GET("/boundary/std", func(ctx httpx.Context) error {
				return ctx.Text(http.StatusOK, "handler")
			})
		}, httptest.NewRequest(http.MethodGet, "http://example.com/boundary/std", nil))

		if resp.Status != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%q", resp.Status, resp.Body)
		}
		if resp.Body != "handler" {
			t.Fatalf("body = %q, want %q: the std middleware must continue the chain", resp.Body, "handler")
		}
		wantTrace := `context="from-layer" store=""`
		if trace := resp.Headers.Get("X-Trace"); trace != wantTrace {
			t.Fatalf("the std middleware saw %s, want %s: the value did not cross into the request it was handed, "+
				"or the store value did, which only the standard context carries", trace, wantTrace)
		}
		r.compareGolden(t, resp)
	})
}
