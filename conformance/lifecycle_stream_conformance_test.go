package conformance

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-sphere/httpx"
)

// startWithTimeout runs engine.Start and returns its error, failing the test
// if Start blocks (a closed engine must return immediately).
func startWithTimeout(t *testing.T, engine httpx.Engine) error {
	t.Helper()
	errCh := make(chan error, 1)
	go func() {
		errCh <- engine.Start()
	}()
	select {
	case err := <-errCh:
		return err
	case <-time.After(2 * time.Second):
		t.Fatal("Start did not return; a closed engine must fail immediately")
		return nil
	}
}

// TestEngineSingleUseConformance covers D7: engines are single-use. Start
// after Stop — in either order — must return httpx.ErrEngineClosed on every
// adapter instead of fake-starting (gin/echo), silently restarting (fiber),
// or failing with a framework-specific error (hertz).
func TestEngineSingleUseConformance(t *testing.T) {
	t.Run("StopBeforeStart", func(t *testing.T) {
		for _, name := range conformanceFrameworks {
			t.Run(name, func(t *testing.T) {
				engine := newStartEngine(t, name)
				stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				_ = engine.Stop(stopCtx)

				if err := startWithTimeout(t, engine); !errors.Is(err, httpx.ErrEngineClosed) {
					t.Fatalf("%s Start after Stop = %v, want ErrEngineClosed", name, err)
				}
			})
		}
	})

	t.Run("RestartAfterStop", func(t *testing.T) {
		for _, name := range conformanceFrameworks {
			t.Run(name, func(t *testing.T) {
				engine := newStartEngine(t, name)

				startErrCh := make(chan error, 1)
				go func() {
					startErrCh <- engine.Start()
				}()
				deadline := time.Now().Add(2 * time.Second)
				for time.Now().Before(deadline) && !engine.IsRunning() {
					time.Sleep(10 * time.Millisecond)
				}

				stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				_ = engine.Stop(stopCtx)
				select {
				case err := <-startErrCh:
					if !isExpectedStartExit(err) {
						t.Fatalf("%s first Start returned unexpected error: %v", name, err)
					}
				case <-time.After(3 * time.Second):
					t.Fatalf("%s first Start did not exit after Stop", name)
				}

				if err := startWithTimeout(t, engine); !errors.Is(err, httpx.ErrEngineClosed) {
					t.Fatalf("%s restart = %v, want ErrEngineClosed", name, err)
				}
			})
		}
	})
}

// TestFlusherCapabilityConformance covers C6: gin/echo/hertz expose Flusher,
// fiber (buffered model) does not, and an early Flush must not corrupt the
// final in-process response.
func TestFlusherCapabilityConformance(t *testing.T) {
	wantFlusher := map[string]bool{"ginx": true, "echox": true, "hertzx": true, "fiberx": false}
	for _, name := range conformanceFrameworks {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, name)
			h.Router.GET("/flush/cap", func(ctx httpx.Context) error {
				fl, ok := httpx.AsFlusher(ctx)
				if ok != wantFlusher[name] {
					return httpx.NewInternalServerError("unexpected Flusher capability")
				}
				if ok {
					ctx.SetHeader("Content-Type", "text/plain; charset=utf-8")
					ctx.Status(http.StatusOK)
					if err := fl.Flush(); err != nil {
						return err
					}
				}
				return ctx.Text(http.StatusOK, "after-flush")
			})

			got := h.Do(t, httptest.NewRequest(http.MethodGet, "http://example.com/flush/cap", nil))
			if got.Status != http.StatusOK {
				t.Fatalf("%s status = %d; body=%q", name, got.Status, got.Body)
			}
			if got.Body != "after-flush" {
				t.Fatalf("%s body = %q, want %q", name, got.Body, "after-flush")
			}
		})
	}
}

// TestStreamBufferedConformance covers C6: Stream must produce the same final
// response on every adapter under in-process dispatch.
func TestStreamBufferedConformance(t *testing.T) {
	for _, name := range conformanceFrameworks {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, name)
			h.Router.GET("/stream/buffered", func(ctx httpx.Context) error {
				s, ok := httpx.AsStreamer(ctx)
				if !ok {
					return httpx.NewInternalServerError("Streamer not supported")
				}
				return s.Stream(http.StatusOK, "text/event-stream", func(w io.Writer) error {
					if _, err := io.WriteString(w, "data: one\n\n"); err != nil {
						return err
					}
					_, err := io.WriteString(w, "data: two\n\n")
					return err
				})
			})

			got := h.Do(t, httptest.NewRequest(http.MethodGet, "http://example.com/stream/buffered", nil))
			if got.Status != http.StatusOK {
				t.Fatalf("%s status = %d; body=%q", name, got.Status, got.Body)
			}
			if got.Body != "data: one\n\ndata: two\n\n" {
				t.Fatalf("%s body = %q", name, got.Body)
			}
			if ct := got.Headers.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
				t.Fatalf("%s content-type = %q, want text/event-stream", name, ct)
			}
		})
	}
}

// TestStreamIncrementalDeliveryConformance covers C6 end to end: over a real
// connection, data written before the handler (or stream callback) finishes
// must reach the client. The handler blocks on a gate after the first event;
// the test only opens the gate once the first event has been read, so the
// test can only pass when flushing actually works mid-stream.
func TestStreamIncrementalDeliveryConformance(t *testing.T) {
	for _, name := range conformanceFrameworks {
		t.Run(name, func(t *testing.T) {
			addr := reserveAddrTB(t)
			engine := newTrustedProxyEngine(t, name, addr, nil)

			gate := make(chan struct{})
			var once sync.Once
			openGate := func() { once.Do(func() { close(gate) }) }
			defer openGate()

			engine.Group("").GET("/sse", func(ctx httpx.Context) error {
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

			stop := startEngineAndWait(t, engine, addr)
			defer stop()

			reqCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, "http://"+addr+"/sse", nil)
			if err != nil {
				t.Fatalf("build request: %v", err)
			}
			resp, err := (&http.Client{}).Do(req)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("%s status = %d", name, resp.StatusCode)
			}

			reader := bufio.NewReader(resp.Body)
			first, err := reader.ReadString('\n')
			if err != nil {
				t.Fatalf("%s reading first event: %v", name, err)
			}
			if first != "data: one\n" {
				t.Fatalf("%s first line = %q", name, first)
			}
			// First event arrived while the handler is still blocked: flushing
			// works. Release the handler and drain the rest.
			openGate()
			rest, err := io.ReadAll(reader)
			if err != nil {
				t.Fatalf("%s reading remainder: %v", name, err)
			}
			if !strings.Contains(string(rest), "data: two") {
				t.Fatalf("%s remainder = %q, want it to contain %q", name, rest, "data: two")
			}
		})
	}
}
