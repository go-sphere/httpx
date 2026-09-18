package stdx

import (
	"bufio"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-sphere/httpx"
)

// The shared suite (httpxtest) pins what every adapter must agree on. The
// tests in this package pin what is specific to this adapter: the pool, the
// writer wrapper, the net/http bridge, the tree, and the form decoders — the
// parts a refactor of context.go or engine.go can break without any other
// adapter noticing.

func newTestEngine(tb testing.TB, opts ...Option) (*Engine, *Router) {
	tb.Helper()
	engine, ok := New(opts...).(*Engine)
	if !ok {
		tb.Fatal("New did not return *Engine")
	}
	router, ok := engine.Group("").(*Router)
	if !ok {
		tb.Fatal("Group did not return *Router")
	}
	return engine, router
}

func serve(engine *Engine, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

func getReq(target string) *http.Request {
	return httptest.NewRequest(http.MethodGet, "http://example.com"+target, nil)
}

func bodyReq(method, target, contentType, body string) *http.Request {
	req := httptest.NewRequest(method, "http://example.com"+target, strings.NewReader(body))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	return req
}

// trace records the order layers ran in, which is how chain tests state their
// expectations as one string.
type trace struct{ steps []string }

func (tr *trace) String() string { return strings.Join(tr.steps, ",") }

func (tr *trace) reset() { tr.steps = tr.steps[:0] }

func (tr *trace) mw(name string) httpx.Middleware {
	return func(ctx httpx.Context) error {
		tr.steps = append(tr.steps, name)
		return ctx.Next()
	}
}

func (tr *trace) interceptor(name string) httpx.Interceptor {
	return func(next httpx.Handler) httpx.Handler {
		return func(ctx httpx.Context) error {
			tr.steps = append(tr.steps, name)
			return next(ctx)
		}
	}
}

func (tr *trace) leaf(name string) httpx.Handler {
	return func(ctx httpx.Context) error {
		tr.steps = append(tr.steps, name)
		return ctx.NoContent(http.StatusNoContent)
	}
}

// hijackRecorder is a ResponseRecorder that can also be hijacked, which the
// wrapper's Hijack path and the bridged middleware's Hijack path both need.
type hijackRecorder struct {
	*httptest.ResponseRecorder
	hijacked bool
}

func (h *hijackRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h.hijacked = true
	client, server := net.Pipe()
	_ = server.Close()
	return client, bufio.NewReadWriter(bufio.NewReader(client), bufio.NewWriter(client)), nil
}

func writeFile(dir, name, content string) error {
	return os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600)
}
