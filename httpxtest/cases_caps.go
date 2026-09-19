package httpxtest

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-sphere/httpx"
)

func init() {
	register("Caps", casesCaps)
}

// A capability declaration is a claim about the adapter, so the suite checks
// the claim itself: an adapter that declares a feature must expose it, and one
// that does not must not. Without this, Caps would be documentation that drifts.
func casesCaps(t *testing.T, r runner) {
	t.Run("NamedWildcardIsDeclaredHonestly", func(t *testing.T) {
		var reported bool
		r.serve(t, func(router httpx.Router) {
			reported = router.SupportsRouterFeature(httpx.RouterFeatureNamedWildcard)
			router.GET("/caps/wildcard", func(ctx httpx.Context) error {
				return ctx.NoContent(http.StatusNoContent)
			})
		}, httptest.NewRequest(http.MethodGet, "http://example.com/caps/wildcard", nil))

		if reported != r.suite.Caps.NamedWildcard {
			t.Fatalf("SupportsRouterFeature(NamedWildcard) = %v, but Caps.NamedWildcard = %v",
				reported, r.suite.Caps.NamedWildcard)
		}
	})

	// The wildcard *param* works on every adapter regardless of the feature
	// flag: those without native named wildcards normalize the path at
	// registration and keep Param(name) working. The flag reports only whether
	// the framework has them natively.
	t.Run("NamedWildcardParam", func(t *testing.T) {
		r.assertGolden(t, func(router httpx.Router) {
			router.GET("/caps/files/*filepath", func(ctx httpx.Context) error {
				return ctx.JSON(http.StatusOK, map[string]any{
					"filepath": ctx.Param("filepath"),
					"params":   ctx.Params(),
				})
			})
		}, httptest.NewRequest(http.MethodGet, "http://example.com/caps/files/a/b/c.txt", nil))
	})

	t.Run("FlusherIsDeclaredHonestly", func(t *testing.T) {
		var supported bool
		r.serve(t, func(router httpx.Router) {
			router.GET("/caps/flusher", func(ctx httpx.Context) error {
				_, supported = httpx.AsFlusher(ctx)
				return ctx.NoContent(http.StatusNoContent)
			})
		}, httptest.NewRequest(http.MethodGet, "http://example.com/caps/flusher", nil))

		if supported != r.suite.Caps.Flusher {
			t.Fatalf("AsFlusher supported = %v, but Caps.Flusher = %v", supported, r.suite.Caps.Flusher)
		}
	})

	// Streaming is required of every adapter, so it is asserted rather than
	// declared — the flag would have nothing to distinguish.
	t.Run("StreamerIsAlwaysSupported", func(t *testing.T) {
		var supported bool
		r.serve(t, func(router httpx.Router) {
			router.GET("/caps/streamer", func(ctx httpx.Context) error {
				_, supported = httpx.AsStreamer(ctx)
				return ctx.NoContent(http.StatusNoContent)
			})
		}, httptest.NewRequest(http.MethodGet, "http://example.com/caps/streamer", nil))

		if !supported {
			t.Fatalf("%s: Context does not implement httpx.Streamer", r.suite.Name)
		}
	})

	// The native context escape hatch is part of the contract for every adapter,
	// so a case reaching for it must fail loudly rather than silently skip.
	t.Run("NativeContextIsReachable", func(t *testing.T) {
		var native any
		r.serve(t, func(router httpx.Router) {
			router.GET("/caps/native", func(ctx httpx.Context) error {
				if p, ok := ctx.(httpx.NativeContextProvider); ok {
					native = p.NativeContext()
				}
				return ctx.NoContent(http.StatusNoContent)
			})
		}, httptest.NewRequest(http.MethodGet, "http://example.com/caps/native", nil))

		if native == nil {
			t.Fatalf("%s: Context does not expose a native context", r.suite.Name)
		}
	})
}
