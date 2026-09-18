package stdx

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-sphere/httpx"
)

// discardWriter is the cheapest possible ResponseWriter: the point of these
// benchmarks is the adapter's own per-request work, not response encoding.
type discardWriter struct{ header http.Header }

func (w *discardWriter) Header() http.Header         { return w.header }
func (w *discardWriter) Write(p []byte) (int, error) { return len(p), nil }
func (w *discardWriter) WriteHeader(int)             {}

func benchEngine(routes ...string) (http.Handler, httpx.Router) {
	engine := New()
	router := engine.Group("")
	for _, pattern := range routes {
		router.GET(pattern, func(ctx httpx.Context) error {
			return ctx.NoContent(http.StatusNoContent)
		})
	}
	return engine.(http.Handler), router
}

// BenchmarkServeStatic is the Empty scenario: routing plus the per-request
// context, with no handler work at all.
func BenchmarkServeStatic(b *testing.B) {
	handler, _ := benchEngine("/scenario")
	req := httptest.NewRequest(http.MethodGet, "http://example.com/scenario", nil)
	w := &discardWriter{header: make(http.Header)}
	b.ReportAllocs()
	for b.Loop() {
		clear(w.header)
		handler.ServeHTTP(w, req)
	}
}

// BenchmarkServeParam adds two parameters, which is the shape a generated
// handler is routed with.
func BenchmarkServeParam(b *testing.B) {
	handler, _ := benchEngine("/users/:id/posts/:post")
	req := httptest.NewRequest(http.MethodGet, "http://example.com/users/42/posts/7", nil)
	w := &discardWriter{header: make(http.Header)}
	b.ReportAllocs()
	for b.Loop() {
		clear(w.header)
		handler.ServeHTTP(w, req)
	}
}

// BenchmarkMatchOnly isolates the tree from everything else.
func BenchmarkMatchOnly(b *testing.B) {
	root := &node{}
	for _, pattern := range []string{"/scenario", "/users/:id", "/users/:id/posts/:post", "/assets/*filepath"} {
		root.add(http.MethodGet, pattern, &route{pattern: pattern})
	}
	var buf [8]string
	b.Run("static", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			values := buf[:0]
			if r, _ := root.match(http.MethodGet, "/scenario", &values); r == nil {
				b.Fatal("no match")
			}
		}
	})
	b.Run("twoParams", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			values := buf[:0]
			if r, _ := root.match(http.MethodGet, "/users/42/posts/7", &values); r == nil {
				b.Fatal("no match")
			}
		}
	})
}

// BenchmarkContextLifecycle isolates the pool and the reset from routing.
func BenchmarkContextLifecycle(b *testing.B) {
	engine := New().(*Engine)
	req := httptest.NewRequest(http.MethodGet, "http://example.com/scenario", nil)
	w := &discardWriter{header: make(http.Header)}
	b.ReportAllocs()
	for b.Loop() {
		ctx, _ := engine.pool.Get().(*stdContext)
		ctx.reset(w, req)
		engine.pool.Put(ctx)
	}
}
