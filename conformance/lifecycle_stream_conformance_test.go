package conformance

import (
	"bufio"
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-sphere/httpx"
)

// Over a real connection, data written before the stream callback finishes must
// reach the client. The handler blocks on a gate until the first event has been
// read, so the test passes only if flushing actually works mid-stream.
func TestStreamIncrementalDeliveryConformance(t *testing.T) {
	for _, a := range adapters() {
		t.Run(a.Name, func(t *testing.T) {
			assertStreamIncremental(t, a, nil)
		})
	}
}

// A net/http middleware around the route must not turn a stream into one
// buffered response: the first event still arrives while the callback runs.
func TestStreamThroughStdMiddlewareIsIncrementalConformance(t *testing.T) {
	for _, a := range adapters() {
		t.Run(a.Name, func(t *testing.T) {
			if a.StdMiddleware == nil {
				t.Skipf("%s: no StdMiddleware hook declared", a.Name)
			}
			headers := assertStreamIncremental(t, a, a.StdMiddleware(func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("X-Std", "staged")
					next.ServeHTTP(w, r)
				})
			}))
			if got := headers.Get("X-Std"); got != "staged" {
				t.Errorf("X-Std = %q, want %q", got, "staged")
			}
		})
	}
}

// assertStreamIncremental serves a two-event stream behind mw (when non-nil)
// on a real listener and fails unless the first event is read before the
// callback is allowed to finish. It returns the response headers for checks.
func assertStreamIncremental(t *testing.T, a adapter, mw httpx.Middleware) http.Header {
	t.Helper()
	engine, addr := a.serve(t, serveConfig{})
	router := engine.Group("")
	if mw != nil {
		router.Use(mw)
	}

	gate := make(chan struct{})
	var once sync.Once
	openGate := func() { once.Do(func() { close(gate) }) }
	defer openGate()

	router.GET("/sse", func(ctx httpx.Context) error {
		s, ok := httpx.AsStreamer(ctx)
		if !ok {
			return httpx.NewInternalServerError("Streamer not supported")
		}
		return s.Stream(http.StatusOK, "text/event-stream", func(w io.Writer) error {
			if _, err := io.WriteString(w, "data: one\n\n"); err != nil {
				return err
			}
			<-gate
			_, err := io.WriteString(w, "data: two\n\n")
			return err
		})
	})

	startServing(t, engine, addr)

	reqCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, "http://"+addr+"/sse", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	resp, err := oneShotClient(0).Do(req)
	if err != nil || resp == nil {
		t.Fatalf("request failed: %v", err)
		return nil
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	reader := bufio.NewReader(resp.Body)
	first, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("reading first event: %v", err)
	}
	if first != "data: one\n" {
		t.Fatalf("first line = %q", first)
	}
	openGate()
	rest, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("reading remainder: %v", err)
	}
	if !strings.Contains(string(rest), "data: two") {
		t.Fatalf("remainder = %q, want it to contain %q", rest, "data: two")
	}
	return resp.Header
}

// The first flush commits the status line and headers, so a client waiting for
// them — an EventSource opening its connection — gets them before the handler
// writes any body. The handler blocks until the client has the headers; an
// adapter that holds them back until the first body byte times out here.
func TestFlushSendsHeadersBeforeBodyConformance(t *testing.T) {
	for _, a := range adapters() {
		t.Run(a.Name, func(t *testing.T) {
			if !a.Caps.Flusher {
				t.Skip("the context does not implement httpx.Flusher")
			}
			engine, addr := a.serve(t, serveConfig{})
			gate := make(chan struct{})
			var once sync.Once
			openGate := func() { once.Do(func() { close(gate) }) }
			defer openGate()

			engine.Group("").GET("/early", func(ctx httpx.Context) error {
				f, ok := httpx.AsFlusher(ctx)
				if !ok {
					return httpx.NewInternalServerError("the context does not implement httpx.Flusher")
				}
				ctx.SetHeader("X-Early", "1")
				if err := f.Flush(); err != nil {
					return err
				}
				<-gate
				return ctx.Text(http.StatusOK, "late")
			})
			startServing(t, engine, addr)

			reqCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, "http://"+addr+"/early", nil)
			if err != nil {
				t.Fatalf("build request: %v", err)
			}
			resp, err := oneShotClient(0).Do(req)
			if err != nil {
				t.Fatalf("headers did not arrive before the body: %v", err)
			}
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != http.StatusOK || resp.Header.Get("X-Early") != "1" {
				t.Fatalf("status=%d X-Early=%q, want 200 %q", resp.StatusCode, resp.Header.Get("X-Early"), "1")
			}
			openGate()
			if _, err := io.ReadAll(resp.Body); err != nil {
				t.Fatalf("read body: %v", err)
			}
		})
	}
}

// Stream sends the status line and headers before its callback runs, so a
// client gets them while the callback is still waiting for its first event.
func TestStreamSendsHeadersBeforeFirstWriteConformance(t *testing.T) {
	for _, a := range adapters() {
		t.Run(a.Name, func(t *testing.T) {
			engine, addr := a.serve(t, serveConfig{})
			gate := make(chan struct{})
			var once sync.Once
			openGate := func() { once.Do(func() { close(gate) }) }
			defer openGate()

			engine.Group("").GET("/stream/early", func(ctx httpx.Context) error {
				s, ok := httpx.AsStreamer(ctx)
				if !ok {
					return httpx.NewInternalServerError("Streamer not supported")
				}
				return s.Stream(http.StatusOK, "text/event-stream", func(w io.Writer) error {
					<-gate
					_, err := io.WriteString(w, "data: late\n\n")
					return err
				})
			})
			startServing(t, engine, addr)

			reqCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, "http://"+addr+"/stream/early", nil)
			if err != nil {
				t.Fatalf("build request: %v", err)
			}
			resp, err := oneShotClient(0).Do(req)
			if err != nil {
				t.Fatalf("headers did not arrive before the first write: %v", err)
			}
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "text/event-stream" {
				t.Fatalf("status=%d Content-Type=%q", resp.StatusCode, resp.Header.Get("Content-Type"))
			}
			openGate()
			body, err := io.ReadAll(resp.Body)
			if err != nil || !strings.Contains(string(body), "data: late") {
				t.Fatalf("body=%q err=%v", body, err)
			}
		})
	}
}
