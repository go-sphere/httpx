package ginx

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// The gin-specific benchmarks live in this module rather than in the
// four-framework conformance module: they compare ginx with native gin, so
// they belong next to the adapter they measure. The cross-framework
// comparison table stays in conformance/.

// benchmarkRunner serves one request repeatedly through h, reusing the request
// and a writer that drops bodies, so what is measured is the chain and not the
// response buffer. The response is validated once, before timing.
func benchmarkRunner(tb testing.TB, h http.Handler, path string, wantStatus int) func() {
	tb.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	w := &discardWriter{header: make(http.Header)}
	h.ServeHTTP(w, req)
	if w.status != wantStatus {
		tb.Fatalf("status = %d, want %d", w.status, wantStatus)
	}
	return func() {
		clear(w.header)
		h.ServeHTTP(w, req)
	}
}
