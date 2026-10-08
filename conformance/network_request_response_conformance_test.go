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
	"github.com/go-sphere/httpx/echox"
	"github.com/go-sphere/httpx/fiberx"
	"github.com/go-sphere/httpx/ginx"
	"github.com/go-sphere/httpx/hertzx"
	"github.com/go-sphere/httpx/stdx"
)

// stdMiddlewareAdapters maps each framework of proxyFrameworks to its
// AdaptStdMiddleware.
var stdMiddlewareAdapters = map[string]func(func(http.Handler) http.Handler) httpx.Middleware{
	"ginx":   ginx.AdaptStdMiddleware,
	"fiberx": fiberx.AdaptStdMiddleware,
	"echox":  echox.AdaptStdMiddleware,
	"hertzx": hertzx.AdaptStdMiddleware,
	"stdx":   stdx.AdaptStdMiddleware,
}

func doNetworkRequest(t *testing.T, method, url string, body io.Reader) (*http.Response, string) {
	t.Helper()
	reqCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, method, url, body)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	resp, err := (&http.Client{}).Do(req)
	if err != nil || resp == nil {
		t.Fatalf("request failed: %v", err)
		return nil, ""
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return resp, string(b)
}

// BodyReader must yield the request body a real server received. The
// in-process requesters can hand the body over as a stream where a server
// buffers it, so only a real listener shows what a handler gets in production.
func TestBodyReaderOverNetworkConformance(t *testing.T) {
	for _, name := range proxyFrameworks {
		t.Run(name, func(t *testing.T) {
			addr := reserveAddrTB(t)
			engine := newTrustedProxyEngine(t, name, addr, nil)
			engine.Group("").POST("/echo", func(ctx httpx.Context) error {
				rc := ctx.BodyReader()
				defer func() { _ = rc.Close() }()
				b, err := io.ReadAll(rc)
				if err != nil {
					return err
				}
				return ctx.Text(http.StatusOK, string(b))
			})
			stop := startEngineAndWait(t, engine, addr)
			defer stop()

			resp, body := doNetworkRequest(t, http.MethodPost, "http://"+addr+"/echo", strings.NewReader("hello body"))
			if resp.StatusCode != http.StatusOK || body != "hello body" {
				t.Fatalf("%s: status=%d body=%q, want 200 %q", name, resp.StatusCode, body, "hello body")
			}
		})
	}
}

// Headers a net/http middleware sets before calling next must reach the client
// on a streamed response too, where the header block leaves before the handler
// returns.
func TestStdMiddlewareHeadersReachStreamConformance(t *testing.T) {
	for _, name := range proxyFrameworks {
		t.Run(name, func(t *testing.T) {
			addr := reserveAddrTB(t)
			engine := newTrustedProxyEngine(t, name, addr, nil)
			router := engine.Group("")
			router.Use(stdMiddlewareAdapters[name](func(next http.Handler) http.Handler {
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
			stop := startEngineAndWait(t, engine, addr)
			defer stop()

			resp, body := doNetworkRequest(t, http.MethodGet, "http://"+addr+"/sse", nil)
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("%s: status = %d", name, resp.StatusCode)
			}
			if got := resp.Header.Values("X-Std"); len(got) != 1 || got[0] != "staged" {
				t.Errorf("%s: X-Std = %q, want exactly [\"staged\"]", name, got)
			}
			if got := resp.Header.Values("X-Both"); len(got) != 1 || got[0] != "handler" {
				t.Errorf("%s: X-Both = %q, want the handler's value only", name, got)
			}
			if got := resp.Header.Values("Content-Type"); len(got) != 1 || got[0] != "text/event-stream" {
				t.Errorf("%s: Content-Type = %q, want the stream's [\"text/event-stream\"]", name, got)
			}
			if body != "data: one\n\n" {
				t.Errorf("%s: body = %q, want %q", name, body, "data: one\n\n")
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
	for _, name := range proxyFrameworks {
		t.Run(name, func(t *testing.T) {
			addr := reserveAddrTB(t)
			engine := newTrustedProxyEngine(t, name, addr, nil)
			router := engine.Group("")
			router.Use(stdMiddlewareAdapters[name](func(next http.Handler) http.Handler {
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
			stop := startEngineAndWait(t, engine, addr)
			defer stop()

			reqCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, "http://"+addr+"/sse", nil)
			if err != nil {
				t.Fatal(err)
			}
			resp, err := (&http.Client{Transport: &http.Transport{DisableCompression: true}}).Do(req)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			defer func() { _ = resp.Body.Close() }()
			var body io.Reader = resp.Body
			if resp.Header.Get("Content-Encoding") == "gzip" {
				zr, err := gzip.NewReader(resp.Body)
				if err != nil {
					t.Fatalf("%s: Content-Encoding is gzip but the body is not: %v", name, err)
				}
				body = zr
			}
			b, err := io.ReadAll(body)
			if err != nil {
				t.Fatalf("%s: read body (Content-Encoding %q): %v", name, resp.Header.Get("Content-Encoding"), err)
			}
			if string(b) != "data: one\n\n" {
				t.Errorf("%s: decoded body = %q (Content-Encoding %q), want %q", name, b, resp.Header.Get("Content-Encoding"), "data: one\n\n")
			}
		})
	}
}
