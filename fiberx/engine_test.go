package fiberx

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-sphere/httpx"
)

// A nil handler is ignored rather than replacing the configured one.
func TestWithErrorHandlerNilKeepsPrevious(t *testing.T) {
	engine := New(
		WithErrorHandler(func(ctx httpx.Context, err error) {
			_ = ctx.Text(http.StatusTeapot, "custom")
		}),
		WithErrorHandler(nil),
	)
	engine.Group("").GET("/fail", func(httpx.Context) error {
		return errors.New("boom")
	})
	resp, err := engine.(*Engine).Do(httptest.NewRequest(http.MethodGet, "/fail", nil))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusTeapot || string(body) != "custom" {
		t.Fatalf("status=%d body=%q, want the first handler's 418 %q", resp.StatusCode, body, "custom")
	}
}
