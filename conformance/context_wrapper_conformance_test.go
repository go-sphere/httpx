package conformance

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/gin-gonic/gin"
	"github.com/go-sphere/httpx"
	"github.com/go-sphere/httpx/echox"
	"github.com/go-sphere/httpx/fiberx"
	"github.com/go-sphere/httpx/ginx"
	"github.com/go-sphere/httpx/hertzx"
	"github.com/gofiber/fiber/v3"
	"github.com/labstack/echo/v4"
)

// A Context received by an inner handler must remain usable by outer
// middleware after Next returns. Native middleware must share the same request.
func TestContextWrappersSurviveUnwind(t *testing.T) {
	for _, name := range conformanceFrameworks {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, name)
			var inner, leaf httpx.Context
			var order []string
			h.Router.Use(func(c httpx.Context) error {
				defer func() { inner, leaf = nil, nil }()
				order = append(order, "outer-before")
				err := c.Next()
				if inner == nil || leaf == nil {
					t.Error("downstream did not execute")
					return err
				}
				for _, retained := range []httpx.Context{inner, leaf} {
					if retained.Param("id") != "42" {
						t.Error("retained context lost route parameters")
					}
					if retained.StatusCode() != 204 {
						t.Error("retained context lost response status")
					}
					a, aok := httpx.AsNativeContext[any](c)
					b, bok := httpx.AsNativeContext[any](retained)
					if !aok || !bok || a != b {
						t.Error("native context identity changed")
					}
					retained.Set("after", "still-valid")
					if v, ok := c.Get("after"); !ok || v != "still-valid" {
						t.Error("retained context lost shared state")
					}
				}
				order = append(order, "outer-after")
				return err
			})
			h.Router.Use(nativeHeaderMiddleware(t, name, "X-Native", "yes"))
			h.Router.Use(func(c httpx.Context) error {
				inner = c
				order = append(order, "inner-before")
				err := c.Next()
				order = append(order, "inner-after")
				return err
			})
			h.Router.GET("/unwind/:id", func(c httpx.Context) error {
				leaf = c
				order = append(order, "leaf")
				return c.NoContent(204)
			})
			for range 3 {
				order = nil
				got := h.Do(t, httptest.NewRequest(http.MethodGet, "/unwind/42", nil))
				if got.Status != 204 || got.Headers.Get("X-Native") != "yes" {
					t.Fatalf("unexpected response: %+v", got)
				}
				expected := []string{"outer-before", "inner-before", "leaf", "inner-after", "outer-after"}
				if !reflect.DeepEqual(order, expected) {
					t.Fatalf("order = %v, want %v", order, expected)
				}
			}
		})
	}
}

// Request access stays shared when a value-backed wrapper is copied into an
// interface. In particular SetContext must update the native request.
func TestWrapperSetContextAfterNext(t *testing.T) {
	type contextKey struct{}
	for _, name := range conformanceFrameworks {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, name)
			h.Router.Use(func(c httpx.Context) error {
				c.SetContext(context.WithValue(c.Context(), contextKey{}, "outer"))
				err := c.Next()
				if name != "hertzx" && c.Context().Value(contextKey{}) != "handler" {
					t.Error("SetContext did not update the shared native request")
				}
				// Hertz intentionally retains a separate baseCtx for each invocation.
				if name == "hertzx" && c.Context().Value(contextKey{}) != "outer" {
					t.Error("Hertz middleware base context changed")
				}
				return err
			})
			h.Router.GET("/context", func(c httpx.Context) error {
				if c.Context().Value(contextKey{}) != "outer" {
					t.Error("handler did not receive middleware context")
				}
				c.SetContext(context.WithValue(c.Context(), contextKey{}, "handler"))
				return c.NoContent(204)
			})
			got := h.Do(t, httptest.NewRequest(http.MethodGet, "/context", nil))
			if got.Status != 204 {
				t.Fatalf("status = %d", got.Status)
			}
		})
	}
}

// nativeHeaderMiddleware wraps each framework's own middleware so the case can
// check that a retained httpx.Context still works across a native layer. The
// portable parts of native-middleware behavior live in the shared suite; this
// file keeps the part that reaches for framework types.
func nativeHeaderMiddleware(t *testing.T, framework, key, value string) httpx.Middleware {
	t.Helper()
	switch framework {
	case "ginx":
		return ginx.AdaptGinMiddleware(func(c *gin.Context) { c.Header(key, value) })
	case "fiberx":
		return fiberx.AdaptFiberMiddleware(func(c fiber.Ctx) error {
			c.Set(key, value)
			return c.Next()
		})
	case "echox":
		return echox.AdaptEchoMiddleware(func(next echo.HandlerFunc) echo.HandlerFunc {
			return func(c echo.Context) error {
				c.Response().Header().Set(key, value)
				return next(c)
			}
		})
	case "hertzx":
		return hertzx.AdaptHertzMiddleware(func(_ context.Context, rc *app.RequestContext) {
			rc.Header(key, value)
		})
	default:
		t.Fatalf("unknown framework %q", framework)
		return nil
	}
}
