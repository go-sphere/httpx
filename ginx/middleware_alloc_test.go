//go:build !race

package ginx

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-sphere/httpx"
)

// The composed chain holds no per-request state, so a request through it
// allocates nothing beyond the bare route.
func TestComposedChainAllocatesNothing(t *testing.T) {
	pass := func(next httpx.Handler) httpx.Handler {
		return func(ctx httpx.Context) error { return next(ctx) }
	}
	leaf := func(ctx httpx.Context) error { return ctx.NoContent(http.StatusNoContent) }

	ge, app := newChainEngine(t)
	app.Use(pass, pass)
	deep := app.Group("/a")
	deep.Use(pass)
	deeper := deep.Group("/b")
	deeper.Use(pass)
	deeper.GET("/route", leaf)

	req := httptest.NewRequest(http.MethodGet, "/a/b/route", nil)
	w := &discardWriter{header: make(http.Header)}
	ge.ServeHTTP(w, req)
	if w.status != http.StatusNoContent {
		t.Fatalf("status = %d", w.status)
	}
	if allocs := testing.AllocsPerRun(50, func() { ge.ServeHTTP(w, req) }); allocs != 0 {
		t.Fatalf("%v allocations per request through a composed chain, want 0", allocs)
	}
}

// The unmatched-path chain is composed when the engine's layer list changes,
// not per request, so adding layers must not add allocations to a 404: the
// fallback cannot snapshot its chain (installRouteFallback runs inside New,
// before any Use call).
func TestUnmatchedPathChainAllocatesNothingExtra(t *testing.T) {
	pass := func(next httpx.Handler) httpx.Handler {
		return func(ctx httpx.Context) error { return next(ctx) }
	}

	measure := func(layers int) float64 {
		ge, app := newChainEngine(t)
		for range layers {
			app.Use(pass)
		}
		app.Group("").GET("/known", func(ctx httpx.Context) error {
			return ctx.NoContent(http.StatusNoContent)
		})

		req := httptest.NewRequest(http.MethodGet, "/nope", nil)
		w := &discardWriter{header: make(http.Header)}
		ge.ServeHTTP(w, req)
		if w.status != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", w.status)
		}
		return testing.AllocsPerRun(50, func() { ge.ServeHTTP(w, req) })
	}

	bare := measure(0)
	if deep := measure(8); deep != bare {
		t.Fatalf("404 through an 8-layer engine chain allocates %v, bare 404 allocates %v: the composition is being rebuilt per request",
			deep, bare)
	}
	t.Logf("404 allocations: %v with no middleware, %v with eight", bare, measure(8))
}
