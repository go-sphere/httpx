//go:build !race

package stdx

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-sphere/httpx"
)

// Error responses are what every probe of an unknown path gets, so they are
// held to the same budget as a successful one: no allocation of the adapter's
// own. Not under -race, which drops sync.Pool items at random.
func TestErrorPathsDoNotAllocate(t *testing.T) {
	notFound := httpx.NewNotFoundError("missing")
	e := New()
	e.Group("").GET("/fail", func(httpx.Context) error { return notFound })
	h := e.(http.Handler)
	for _, target := range []string{"/fail", "/nope"} {
		req := httptest.NewRequest(http.MethodGet, "http://example.com"+target, nil)
		w := &discardWriter{header: make(http.Header)}
		h.ServeHTTP(w, req)
		if n := testing.AllocsPerRun(200, func() {
			clear(w.header)
			h.ServeHTTP(w, req)
		}); n != 0 {
			t.Errorf("%s: %v allocations per request, want 0", target, n)
		}
	}
}
