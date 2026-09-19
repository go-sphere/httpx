package ginx

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// benchmarkRunner serves one request repeatedly through h, reusing the request
// and a writer that drops bodies so the measurement is the chain, not the
// response buffer; the status is validated once, before timing.
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
