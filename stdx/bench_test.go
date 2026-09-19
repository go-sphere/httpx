package stdx

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-sphere/httpx"
)

// discardWriter is the cheapest possible ResponseWriter: these benchmarks
// measure the adapter's own per-request work, not response encoding.
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
// context, no handler work.
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

// BenchmarkServeParam adds two parameters, the shape a generated handler is
// routed with.
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

// proxiedRequest is the shape the two benchmarks below measure: a handful of
// headers and a three-hop forwarding chain behind two trusted proxies.
func proxiedRequest() *http.Request {
	req := httptest.NewRequest(http.MethodGet, "http://example.com/x", nil)
	req.Header.Set("Authorization", "Bearer abc")
	req.Header.Set("X-Request-Id", "rid-1")
	req.Header.Set("X-Trace", "t-1")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "bench")
	req.Header.Set("X-Forwarded-For", "203.0.113.7, 10.0.0.1, 10.0.0.2")
	req.RemoteAddr = "10.0.0.9:1234"
	return req
}

type headerDTO struct {
	Auth      string `header:"Authorization"`
	RequestID string `header:"X-Request-Id"`
	Trace     string `header:"X-Trace"`
}

// BenchmarkBindHeader is the binder a generated handler reaches for most often.
// It hands the decoder the live header map: copying it first cost a map and a
// slice per header, which was twenty times what routing the request costs.
func BenchmarkBindHeader(b *testing.B) {
	c := &stdContext{}
	c.native.c = c
	c.reset(&discardWriter{header: make(http.Header)}, proxiedRequest())
	b.ReportAllocs()
	for b.Loop() {
		var dst headerDTO
		if err := c.BindHeader(&dst); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkClientIP walks a forwarding chain under a trusted-proxy policy —
// the work any access log or rate limiter does per request.
func BenchmarkClientIP(b *testing.B) {
	engine := New().(*Engine)
	engine.trustedProxies = benchProxies(b)
	req := proxiedRequest()
	b.ReportAllocs()
	for b.Loop() {
		if engine.clientIP(req) != "203.0.113.7" {
			b.Fatal("wrong client IP")
		}
	}
}

func benchProxies(tb testing.TB) []*net.IPNet {
	tb.Helper()
	nets, err := httpx.ParseCIDRs([]string{"10.0.0.0/8"})
	if err != nil {
		tb.Fatal(err)
	}
	return nets
}

// TestClientIPDoesNotAllocate guards the in-place right-to-left walk over
// X-Forwarded-For. Splitting the header and reversing the result allocated
// twice on a path every request takes.
func TestClientIPDoesNotAllocate(t *testing.T) {
	engine := New().(*Engine)
	engine.trustedProxies = benchProxies(t)
	req := proxiedRequest()
	if got := engine.clientIP(req); got != "203.0.113.7" {
		t.Fatalf("clientIP = %q, want %q", got, "203.0.113.7")
	}
	if n := testing.AllocsPerRun(100, func() { engine.clientIP(req) }); n != 0 {
		t.Fatalf("clientIP allocated %v times per call, want 0", n)
	}
}
