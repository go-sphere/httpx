package conformance

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-sphere/httpx"
	"github.com/go-sphere/httpx/httpxtest"
)

func countOpenFDs(t *testing.T) int {
	t.Helper()
	entries, err := os.ReadDir("/dev/fd")
	if err != nil {
		t.Skipf("cannot read /dev/fd: %v", err)
	}
	return len(entries)
}

func TestAdversarialConcurrentRequestsAllAdapters(t *testing.T) {
	tmpDir := t.TempDir()
	staticFile := filepath.Join(tmpDir, "sample.txt")
	staticContent := []byte("static-adversarial-test-payload-1234567890")
	if err := os.WriteFile(staticFile, staticContent, 0o600); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	for _, a := range adapters() {
		t.Run(a.Name, func(t *testing.T) {
			engine := a.NewEngine(t, httpxtest.Options{})
			router := engine.Group("")

			router.GET("/api/ping", func(ctx httpx.Context) error {
				q := ctx.Query("q")
				return ctx.JSON(http.StatusOK, map[string]string{
					"message": "pong",
					"query":   q,
				})
			})

			router.HEAD("/api/head", func(ctx httpx.Context) error {
				ctx.SetHeader("X-Custom-Test", "adversarial-head")
				return ctx.NoContent(http.StatusOK)
			})

			router.POST("/api/echo", func(ctx httpx.Context) error {
				body, err := ctx.BodyRaw()
				if err != nil {
					return ctx.Text(http.StatusBadRequest, err.Error())
				}
				return ctx.Text(http.StatusOK, string(body))
			})

			router.StaticFS("/static", os.DirFS(tmpDir))

			const concurrency = 60
			const reqsPerWorker = 30
			var wg sync.WaitGroup
			errCh := make(chan error, concurrency)
			for worker := range concurrency {
				wg.Go(func() {
					for i := range reqsPerWorker {
						if err := adversarialRequest(engine, worker, i, string(staticContent)); err != nil {
							errCh <- fmt.Errorf("worker %d request %d: %w", worker, i, err)
							return
						}
					}
				})
			}
			wg.Wait()
			close(errCh)

			for err := range errCh {
				t.Errorf("concurrent failure: %v", err)
			}
		})
	}
}

// adversarialRequest sends request i of a worker, cycling through the routes
// of TestAdversarialConcurrentRequestsAllAdapters, and checks the response.
func adversarialRequest(engine httpx.Engine, worker, i int, staticContent string) error {
	var req *http.Request
	var check func(responseSnapshot) error
	switch i % 5 {
	case 0:
		wantQuery := fmt.Sprintf("w%d-i%d", worker, i)
		req = httptest.NewRequest(http.MethodGet, "http://example.com/api/ping?q="+wantQuery, nil)
		check = func(resp responseSnapshot) error {
			if resp.Status != http.StatusOK {
				return fmt.Errorf("GET status: got %d, want 200", resp.Status)
			}
			var got map[string]string
			if err := json.Unmarshal([]byte(resp.Body), &got); err != nil {
				return fmt.Errorf("GET body json: %w; body=%q", err, resp.Body)
			}
			if got["message"] != "pong" || got["query"] != wantQuery {
				return fmt.Errorf("GET body=%v, want message=pong query=%q", got, wantQuery)
			}
			return nil
		}
	case 1:
		req = httptest.NewRequest(http.MethodHead, "http://example.com/api/head", nil)
		check = func(resp responseSnapshot) error {
			if resp.Status != http.StatusOK && resp.Status != http.StatusNoContent {
				return fmt.Errorf("HEAD status: got %d", resp.Status)
			}
			if got := resp.Headers.Get("X-Custom-Test"); got != "adversarial-head" {
				return fmt.Errorf("HEAD header missing: got %q", got)
			}
			return nil
		}
	case 2:
		payload := fmt.Sprintf("echo-payload-w%d-i%d", worker, i)
		req = httptest.NewRequest(http.MethodPost, "http://example.com/api/echo", strings.NewReader(payload))
		check = func(resp responseSnapshot) error {
			if resp.Status != http.StatusOK || resp.Body != payload {
				return fmt.Errorf("POST: got %d %q, want 200 %q", resp.Status, resp.Body, payload)
			}
			return nil
		}
	case 3:
		req = httptest.NewRequest(http.MethodGet, "http://example.com/static/sample.txt", nil)
		check = func(resp responseSnapshot) error {
			if resp.Status != http.StatusOK || resp.Body != staticContent {
				return fmt.Errorf("static GET: got %d %q", resp.Status, resp.Body)
			}
			return nil
		}
	default:
		// HEAD static: 200 with an empty body on every adapter.
		req = httptest.NewRequest(http.MethodHead, "http://example.com/static/sample.txt", nil)
		check = func(resp responseSnapshot) error {
			if resp.Status != http.StatusOK || resp.Body != "" {
				return fmt.Errorf("static HEAD: got %d %q, want 200 and no body", resp.Status, resp.Body)
			}
			return nil
		}
	}
	resp, err := serveInProcess(engine, req)
	if err != nil {
		return err
	}
	return check(resp)
}

func TestAdversarialStaticHEADZeroFDLeaks(t *testing.T) {
	tmpDir := t.TempDir()
	staticFile := filepath.Join(tmpDir, "leak_check.txt")
	staticContent := []byte("fd-leak-verification-payload-9876543210")
	if err := os.WriteFile(staticFile, staticContent, 0o600); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	for _, a := range adapters() {
		t.Run(a.Name, func(t *testing.T) {
			engine := a.NewEngine(t, httpxtest.Options{})
			engine.Group("").StaticFS("/files", os.DirFS(tmpDir))

			// headStatic serves one HEAD request for the file and reports any
			// deviation as an error, so workers can call it.
			headStatic := func() error {
				req := httptest.NewRequest(http.MethodHead, "http://example.com/files/leak_check.txt", nil)
				resp, err := serveInProcess(engine, req)
				if err != nil {
					return err
				}
				if resp.Status != http.StatusOK || len(resp.Body) != 0 {
					return fmt.Errorf("HEAD: status %d with %d body bytes, want 200 and no body", resp.Status, len(resp.Body))
				}
				return nil
			}
			hammer := func(workers, perWorker int) {
				t.Helper()
				var wg sync.WaitGroup
				errCh := make(chan error, workers)
				for range workers {
					wg.Go(func() {
						for range perWorker {
							if err := headStatic(); err != nil {
								errCh <- err
								return
							}
						}
					})
				}
				wg.Wait()
				close(errCh)
				for err := range errCh {
					t.Fatal(err)
				}
			}

			// Warm up at the measurement concurrency so lazily created runtime FDs
			// (fasthttp workers, pollers) exist before the baseline count.
			hammer(50, 5)

			runtime.GC()
			time.Sleep(50 * time.Millisecond)
			initialFDs := countOpenFDs(t)

			hammer(50, 10)

			// A per-request leak would show ~500; allow 5 for transient runtime FDs
			// (fasthttp workers/timers), re-counting until they settle.
			var diff int
			for attempt := 0; attempt < 5; attempt++ {
				runtime.GC()
				time.Sleep(50 * time.Millisecond)
				diff = countOpenFDs(t) - initialFDs
				if diff <= 5 {
					break
				}
			}
			t.Logf("open FDs before: %d, diff after 500 HEAD requests: %d", initialFDs, diff)
			if diff > 5 {
				t.Fatalf("leaked %d file descriptors (%d open before)", diff, initialFDs)
			}
		})
	}
}
