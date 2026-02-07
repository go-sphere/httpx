package conformance

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-sphere/httpx"
)

func TestCustomErrorHandlerWithoutAbortStillStopsChain(t *testing.T) {
	for _, name := range conformanceFrameworks {
		t.Run(name, func(t *testing.T) {
			h := newCustomErrorHandlerHarness(t, name)

			h.Router.Use(func(ctx httpx.Context) error {
				return errors.New("middleware boom")
			})
			h.Router.Use(func(ctx httpx.Context) error {
				ctx.SetHeader("X-Should-Not-Run", "1")
				return ctx.Next()
			})
			h.Router.GET("/mw/error/custom", func(ctx httpx.Context) error {
				ctx.SetHeader("X-Handler-Ran", "1")
				return ctx.Text(http.StatusOK, "handler-ran")
			})

			got := h.Do(t, httptest.NewRequest(http.MethodGet, "http://example.com/mw/error/custom", nil))

			if got.Status != http.StatusTeapot {
				t.Fatalf("%s status mismatch: want %d, got %d", name, http.StatusTeapot, got.Status)
			}
			if got.Headers.Get("X-Should-Not-Run") != "" {
				t.Fatalf("%s middleware after error should not run", name)
			}
			if got.Headers.Get("X-Handler-Ran") != "" {
				t.Fatalf("%s handler after error should not run", name)
			}

			var payload map[string]string
			if err := json.Unmarshal([]byte(got.Body), &payload); err != nil {
				t.Fatalf("%s parse body failed: %v; body=%q", name, err, got.Body)
			}
			if payload["error"] != "middleware boom" {
				t.Fatalf("%s body mismatch: want error %q, got %q", name, "middleware boom", payload["error"])
			}
		})
	}
}

func newCustomErrorHandlerHarness(tb testing.TB, name string) frameworkHarness {
	tb.Helper()
	b := newFrameworkHarnessTB(tb, name, harnessOptions{mode: harnessModeInProcess, errorMode: harnessErrorTeapot})
	return b.harness
}
