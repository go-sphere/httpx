package fiberx

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-sphere/httpx"
	"github.com/gofiber/fiber/v3"
)

type customFiberContext struct{ *fiber.DefaultCtx }

func (c *customFiberContext) Method(override ...string) string {
	return "custom:" + c.DefaultCtx.Method(override...)
}

func TestCustomContextPreserved(t *testing.T) {
	native := fiber.NewWithCustomCtx(func(app *fiber.App) fiber.CustomCtx {
		return &customFiberContext{DefaultCtx: fiber.NewDefaultCtx(app)}
	})
	engine := New(WithEngine(native))
	r := engine.Group("")
	r.Use(func(next httpx.Handler) httpx.Handler {
		return func(c httpx.Context) error {
			if c.Method() != "custom:GET" {
				t.Errorf("custom Method override lost: %q", c.Method())
			}
			if _, ok := httpx.AsNativeContext[*customFiberContext](c); !ok {
				t.Error("custom native context type lost")
			}
			c.Set("from-middleware", true)
			return next(c)
		}
	})
	r.GET("/custom", func(c httpx.Context) error {
		if c.Method() != "custom:GET" {
			t.Errorf("handler Method override lost: %q", c.Method())
		}
		if v, ok := c.Get("from-middleware"); !ok || v != true {
			t.Error("state not shared with handler")
		}
		return c.NoContent(http.StatusNoContent)
	})
	resp, err := native.Test(httptest.NewRequest(http.MethodGet, "/custom", nil))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}
