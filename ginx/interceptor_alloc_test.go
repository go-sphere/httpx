//go:build !race

package ginx

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-sphere/httpx"
)

// The composed chain holds no per-request state, so a request through it
// allocates nothing beyond what the bare route allocates.
func TestInterceptorAllocatesNothing(t *testing.T) {
	pass := func(next httpx.Handler) httpx.Handler {
		return func(ctx httpx.Context) error { return next(ctx) }
	}
	leaf := func(ctx httpx.Context) error { return ctx.NoContent(http.StatusNoContent) }

	ge, app := newChainEngine(t)
	app.(*Engine).UseInterceptor(pass, pass)
	deep := app.Group("/a")
	deep.(*Router).UseInterceptor(pass)
	deeper := deep.Group("/b")
	deeper.(*Router).UseInterceptor(pass)
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
