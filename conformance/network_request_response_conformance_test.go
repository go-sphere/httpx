package conformance

import (
	"compress/gzip"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/go-sphere/httpx"
)

// oneShotClient closes each connection after its response. A pooled keep-alive
// connection left idle would hold the test's engine.Stop until its deadline on
// hertzx, whose graceful shutdown waits for idle connections too.
func oneShotClient(timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout, Transport: &http.Transport{DisableKeepAlives: true}}
}

func doNetworkRequest(t *testing.T, method, url string, body io.Reader) responseSnapshot {
	t.Helper()
	reqCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, method, url, body)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	resp, err := oneShotClient(0).Do(req)
	if err != nil || resp == nil {
		t.Fatalf("request failed: %v", err)
		return responseSnapshot{}
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return responseSnapshot{Status: resp.StatusCode, Body: string(b), Headers: resp.Header}
}

// BodyReader must yield the request body a real server received. The
// in-process requesters can hand the body over as a stream where a server
// buffers it, so only a real listener shows what a handler gets in production.
func TestBodyReaderOverNetworkConformance(t *testing.T) {
	for _, a := range adapters() {
		t.Run(a.Name, func(t *testing.T) {
			engine, addr := a.serve(t, serveConfig{})
			engine.Group("").POST("/echo", func(ctx httpx.Context) error {
				rc := ctx.BodyReader()
				defer func() { _ = rc.Close() }()
				b, err := io.ReadAll(rc)
				if err != nil {
					return err
				}
				return ctx.Text(http.StatusOK, string(b))
			})
			startServing(t, engine, addr)

			got := doNetworkRequest(t, http.MethodPost, "http://"+addr+"/echo", strings.NewReader("hello body"))
			if got.Status != http.StatusOK || got.Body != "hello body" {
				t.Fatalf("status=%d body=%q, want 200 %q", got.Status, got.Body, "hello body")
			}
		})
	}
}

// Headers a net/http middleware sets before calling next must reach the client
// on a streamed response too, where the header block leaves before the handler
// returns.
func TestStdMiddlewareHeadersReachStreamConformance(t *testing.T) {
	for _, a := range adapters() {
		t.Run(a.Name, func(t *testing.T) {
			engine, addr := a.serve(t, serveConfig{})
			if a.StdMiddleware == nil {
				t.Skipf("%s: no StdMiddleware hook declared", a.Name)
			}
			router := engine.Group("")
			router.Use(a.StdMiddleware(func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("X-Std", "staged")
					w.Header().Set("X-Both", "middleware")
					w.Header().Set("Content-Type", "application/json")
					next.ServeHTTP(w, r)
				})
			}))
			router.GET("/sse", func(ctx httpx.Context) error {
				ctx.SetHeader("X-Both", "handler")
				s, ok := httpx.AsStreamer(ctx)
				if !ok {
					return httpx.NewInternalServerError("Streamer not supported")
				}
				return s.Stream(http.StatusOK, "text/event-stream", func(w io.Writer) error {
					_, err := io.WriteString(w, "data: one\n\n")
					return err
				})
			})
			startServing(t, engine, addr)

			res := doNetworkRequest(t, http.MethodGet, "http://"+addr+"/sse", nil)
			if res.Status != http.StatusOK {
				t.Fatalf("status = %d", res.Status)
			}
			if got := res.Headers.Values("X-Std"); len(got) != 1 || got[0] != "staged" {
				t.Errorf("X-Std = %q, want exactly [\"staged\"]", got)
			}
			if got := res.Headers.Values("X-Both"); len(got) != 1 || got[0] != "handler" {
				t.Errorf("X-Both = %q, want the handler's value only", got)
			}
			if got := res.Headers.Values("Content-Type"); len(got) != 1 || got[0] != "text/event-stream" {
				t.Errorf("Content-Type = %q, want the stream's [\"text/event-stream\"]", got)
			}
			if res.Body != "data: one\n\n" {
				t.Errorf("body = %q, want %q", res.Body, "data: one\n\n")
			}
		})
	}
}

// gzipStdWriter is an eager compression middleware's writer: it announces
// Content-Encoding before next runs and compresses whatever reaches it.
type gzipStdWriter struct {
	http.ResponseWriter
	zw *gzip.Writer
}

func (w gzipStdWriter) Write(b []byte) (int, error) { return w.zw.Write(b) }

func (w gzipStdWriter) Flush() {
	_ = w.zw.Flush()
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// A Content-Encoding a net/http middleware staged before next must describe
// the body the client receives: where a streamed body bypasses the
// middleware's writer, the header must not be sent with it.
func TestStdMiddlewareContentEncodingMatchesStreamConformance(t *testing.T) {
	for _, a := range adapters() {
		t.Run(a.Name, func(t *testing.T) {
			engine, addr := a.serve(t, serveConfig{})
			if a.StdMiddleware == nil {
				t.Skipf("%s: no StdMiddleware hook declared", a.Name)
			}
			router := engine.Group("")
			router.Use(a.StdMiddleware(func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Encoding", "gzip")
					zw := gzip.NewWriter(w)
					next.ServeHTTP(gzipStdWriter{ResponseWriter: w, zw: zw}, r)
					_ = zw.Close()
				})
			}))
			router.GET("/sse", func(ctx httpx.Context) error {
				s, ok := httpx.AsStreamer(ctx)
				if !ok {
					return httpx.NewInternalServerError("Streamer not supported")
				}
				return s.Stream(http.StatusOK, "text/event-stream", func(w io.Writer) error {
					_, err := io.WriteString(w, "data: one\n\n")
					return err
				})
			})
			startServing(t, engine, addr)

			reqCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, "http://"+addr+"/sse", nil)
			if err != nil {
				t.Fatal(err)
			}
			resp, err := (&http.Client{Transport: &http.Transport{DisableCompression: true, DisableKeepAlives: true}}).Do(req)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			defer func() { _ = resp.Body.Close() }()
			var body io.Reader = resp.Body
			if resp.Header.Get("Content-Encoding") == "gzip" {
				zr, err := gzip.NewReader(resp.Body)
				if err != nil {
					t.Fatalf("Content-Encoding is gzip but the body is not: %v", err)
				}
				body = zr
			}
			b, err := io.ReadAll(body)
			if err != nil {
				t.Fatalf("read body (Content-Encoding %q): %v", resp.Header.Get("Content-Encoding"), err)
			}
			if string(b) != "data: one\n\n" {
				t.Errorf("decoded body = %q (Content-Encoding %q), want %q", b, resp.Header.Get("Content-Encoding"), "data: one\n\n")
			}
		})
	}
}
