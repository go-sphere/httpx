package conformance

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/go-sphere/httpx"
)

// countOpenFDs counts open file descriptors by inspecting /dev/fd on Darwin/Linux.
func countOpenFDs(t *testing.T) int {
	t.Helper()
	entries, err := os.ReadDir("/dev/fd")
	if err != nil {
		t.Skipf("cannot read /dev/fd: %v", err)
	}
	return len(entries)
}

// TestAdversarialConcurrentRequestsAllAdapters stress tests concurrent GET, HEAD, POST,
// and static file requests across all 4 adapters (ginx, fiberx, echox, hertzx)
// under -race with 60 concurrent goroutines sending thousands of requests.
func TestAdversarialConcurrentRequestsAllAdapters(t *testing.T) {
	tmpDir := t.TempDir()
	staticFile := filepath.Join(tmpDir, "sample.txt")
	staticContent := []byte("static-adversarial-test-payload-1234567890")
	if err := os.WriteFile(staticFile, staticContent, 0o600); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	for _, name := range conformanceFrameworks {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, name)

			// Register routes
			h.Router.GET("/api/ping", func(ctx httpx.Context) error {
				q := ctx.Query("q")
				return ctx.JSON(http.StatusOK, map[string]string{
					"message": "pong",
					"query":   q,
				})
			})

			h.Router.HEAD("/api/head", func(ctx httpx.Context) error {
				ctx.SetHeader("X-Custom-Test", "adversarial-head")
				return ctx.NoContent(http.StatusOK)
			})

			h.Router.POST("/api/echo", func(ctx httpx.Context) error {
				body, err := ctx.BodyRaw()
				if err != nil {
					return ctx.Text(http.StatusBadRequest, err.Error())
				}
				return ctx.Text(http.StatusOK, string(body))
			})

			h.Router.StaticFS("/static", os.DirFS(tmpDir))

			const concurrency = 60
			const reqsPerWorker = 30
			var wg sync.WaitGroup
			errCh := make(chan error, concurrency*reqsPerWorker)

			wg.Add(concurrency)
			for w := 0; w < concurrency; w++ {
				workerID := w
				go func() {
					defer wg.Done()
					for i := 0; i < reqsPerWorker; i++ {
						switch i % 5 {
						case 0:
							// GET /api/ping
							req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("http://example.com/api/ping?q=w%d-i%d", workerID, i), nil)
							resp := h.Do(t, req)
							if resp.Status != http.StatusOK {
								errCh <- fmt.Errorf("worker %d GET status: got %d, want 200", workerID, resp.Status)
								return
							}
						case 1:
							// HEAD /api/head
							req := httptest.NewRequest(http.MethodHead, "http://example.com/api/head", nil)
							resp := h.Do(t, req)
							if resp.Status != http.StatusOK && resp.Status != http.StatusNoContent {
								errCh <- fmt.Errorf("worker %d HEAD status: got %d", workerID, resp.Status)
								return
							}
							if resp.Headers.Get("X-Custom-Test") != "adversarial-head" {
								errCh <- fmt.Errorf("worker %d HEAD header missing: got %q", workerID, resp.Headers.Get("X-Custom-Test"))
								return
							}
						case 2:
							// POST /api/echo
							payload := fmt.Sprintf("echo-payload-w%d-i%d", workerID, i)
							req := httptest.NewRequest(http.MethodPost, "http://example.com/api/echo", bytes.NewBufferString(payload))
							resp := h.Do(t, req)
							if resp.Status != http.StatusOK {
								errCh <- fmt.Errorf("worker %d POST status: got %d", workerID, resp.Status)
								return
							}
							if resp.Body != payload {
								errCh <- fmt.Errorf("worker %d POST body mismatch: got %q, want %q", workerID, resp.Body, payload)
								return
							}
						case 3:
							// GET static file
							req := httptest.NewRequest(http.MethodGet, "http://example.com/static/sample.txt", nil)
							resp := h.Do(t, req)
							if resp.Status != http.StatusOK {
								errCh <- fmt.Errorf("worker %d static GET status: got %d", workerID, resp.Status)
								return
							}
							if resp.Body != string(staticContent) {
								errCh <- fmt.Errorf("worker %d static GET body mismatch: got %q", workerID, resp.Body)
								return
							}
						case 4:
							// HEAD static file: every adapter must serve HEAD with 200 and an empty body.
							req := httptest.NewRequest(http.MethodHead, "http://example.com/static/sample.txt", nil)
							resp := h.Do(t, req)
							if resp.Status != http.StatusOK {
								errCh <- fmt.Errorf("%s worker %d static HEAD status: got %d", name, workerID, resp.Status)
								return
							}
							if len(resp.Body) != 0 {
								errCh <- fmt.Errorf("%s worker %d static HEAD must have empty body, got %q", name, workerID, resp.Body)
								return
							}
						}
					}
				}()
			}

			wg.Wait()
			close(errCh)

			for err := range errCh {
				t.Errorf("%s concurrent failure: %v", name, err)
			}
		})
	}
}

// TestAdversarialStaticHEADZeroFDLeaks verifies zero file descriptor leaks
// when issuing hundreds of HEAD requests on static file routes.
func TestAdversarialStaticHEADZeroFDLeaks(t *testing.T) {
	tmpDir := t.TempDir()
	staticFile := filepath.Join(tmpDir, "leak_check.txt")
	staticContent := []byte("fd-leak-verification-payload-9876543210")
	if err := os.WriteFile(staticFile, staticContent, 0o600); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	for _, name := range conformanceFrameworks {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, name)
			h.Router.StaticFS("/files", os.DirFS(tmpDir))

			method := http.MethodHead
			expectedStatus := http.StatusOK

			// Warm up the handler at the same concurrency as the measurement so
			// lazily-initialized runtime FDs (fasthttp workers, pollers) are
			// created before the baseline count.
			var warmWg sync.WaitGroup
			warmupFailures := make(chan int, 50*5)
			warmWg.Add(50)
			for w := 0; w < 50; w++ {
				go func() {
					defer warmWg.Done()
					for i := 0; i < 5; i++ {
						warmupReq := httptest.NewRequest(method, "http://example.com/files/leak_check.txt", nil)
						resp := h.Do(t, warmupReq)
						if resp.Status != expectedStatus {
							warmupFailures <- resp.Status
							return
						}
					}
				}()
			}
			warmWg.Wait()
			close(warmupFailures)
			for status := range warmupFailures {
				t.Fatalf("%s warmup status: got %d, want %d", name, status, expectedStatus)
			}

			runtime.GC()
			time.Sleep(50 * time.Millisecond)
			initialFDs := countOpenFDs(t)

			const totalRequests = 500
			const concurrency = 50
			var wg sync.WaitGroup
			errCh := make(chan error, totalRequests)

			wg.Add(concurrency)
			reqsPerWorker := totalRequests / concurrency
			for w := 0; w < concurrency; w++ {
				workerID := w
				go func() {
					defer wg.Done()
					for i := 0; i < reqsPerWorker; i++ {
						req := httptest.NewRequest(method, "http://example.com/files/leak_check.txt", nil)
						r := h.Do(t, req)
						if r.Status != expectedStatus {
							errCh <- fmt.Errorf("%s worker %d req %d got status %d, want %d", name, workerID, i, r.Status, expectedStatus)
							return
						}
						if expectedStatus == http.StatusOK && len(r.Body) != 0 {
							errCh <- fmt.Errorf("%s worker %d req %d expected empty body for HEAD, got %d bytes", name, workerID, i, len(r.Body))
							return
						}
					}
				}()
			}

			wg.Wait()
			close(errCh)

			for err := range errCh {
				t.Fatalf("error during HEAD stress: %v", err)
			}

			// If file descriptors were leaked on each request, diff would be ~500.
			// Allow at most 5 for transient runtime noise (e.g., fasthttp internal
			// workers/timers), re-counting a few times to let transient FDs settle.
			var diff int
			for attempt := 0; attempt < 5; attempt++ {
				runtime.GC()
				time.Sleep(50 * time.Millisecond)
				diff = countOpenFDs(t) - initialFDs
				if diff <= 5 {
					break
				}
			}
			t.Logf("[%s] Open FDs before: %d, diff after 500 HEAD requests: %d", name, initialFDs, diff)
			if diff > 5 {
				t.Fatalf("%s leaked file descriptors! Initial: %d (leaked %d FDs)", name, initialFDs, diff)
			}
		})
	}
}
