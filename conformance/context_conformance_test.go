package conformance

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-sphere/httpx"
)

func TestOptionalContextCapabilitiesConformance(t *testing.T) {
	t.Run("ResponseInfoAfterWrite", func(t *testing.T) {
		results := runAcrossFrameworks(t, func(r httpx.Router) {
			r.Use(func(next httpx.Handler) httpx.Handler {
				return func(ctx httpx.Context) error {
					err := next(ctx)
					if err != nil {
						return err
					}
					// Every adapter must report the status set by a downstream handler.
					if ctx.StatusCode() != http.StatusCreated {
						return errors.New("unexpected status code")
					}
					return nil
				}
			})

			r.GET("/ctx/capabilities/response", func(ctx httpx.Context) error {
				return ctx.JSON(http.StatusCreated, map[string]any{"ok": true})
			})
		}, func() *http.Request {
			return httptest.NewRequest(http.MethodGet, "http://example.com/ctx/capabilities/response", nil)
		})

		for _, name := range conformanceFrameworks {
			got := results[name]
			if got.Status != http.StatusCreated {
				t.Fatalf("%s status mismatch: want %d, got %d", name, http.StatusCreated, got.Status)
			}
		}
	})

	t.Run("NativeContextProvider", func(t *testing.T) {
		results := runAcrossFrameworks(t, func(r httpx.Router) {
			r.GET("/ctx/capabilities/native", func(ctx httpx.Context) error {
				native, ok := httpx.AsNativeContext[any](ctx)
				return ctx.JSON(http.StatusOK, map[string]any{"ok": ok && native != nil})
			})
		}, func() *http.Request {
			return httptest.NewRequest(http.MethodGet, "http://example.com/ctx/capabilities/native", nil)
		})

		for _, name := range conformanceFrameworks {
			got := results[name]
			if got.Status != http.StatusOK {
				t.Fatalf("%s status mismatch: want %d, got %d", name, http.StatusOK, got.Status)
			}
			var payload map[string]any
			if err := json.Unmarshal([]byte(got.Body), &payload); err != nil {
				t.Fatalf("%s parse body failed: %v", name, err)
			}
			v, _ := payload["ok"].(bool)
			if !v {
				t.Fatalf("%s native context capability should be true", name)
			}
		}
	})
}
