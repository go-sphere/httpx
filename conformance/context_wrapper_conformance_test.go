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

// A Context retained by an inner handler must stay usable from outer middleware
// after next returns, including across a native layer.
func TestContextWrappersSurviveUnwind(t *testing.T) {
	for _, name := range conformanceFrameworks {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, name)
			var inner, leaf httpx.Context
			var order []string
			h.Router.Use(func(next httpx.Handler) httpx.Handler {
				return func(c httpx.Context) error {
					defer func() { inner, leaf = nil, nil }()
					order = append(order, "outer-before")
					err := next(c)
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
				}
			})
			useNativeHeader(t, name, h.Router, "X-Native", "yes")
			h.Router.Use(func(next httpx.Handler) httpx.Handler {
				return func(c httpx.Context) error {
					inner = c
					order = append(order, "inner-before")
					err := next(c)
					order = append(order, "inner-after")
					return err
				}
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

// A value-backed wrapper copied into an interface must keep request access
// shared, and SetContext must update the native request.
func TestWrapperSetContextAfterNext(t *testing.T) {
	type contextKey struct{}
	for _, name := range conformanceFrameworks {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, name)
			h.Router.Use(func(next httpx.Handler) httpx.Handler {
				return func(c httpx.Context) error {
					c.SetContext(context.WithValue(c.Context(), contextKey{}, "outer"))
					err := next(c)
					if c.Context().Value(contextKey{}) != "handler" {
						t.Error("SetContext below was not visible to the layer above")
					}
					return err
				}
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

// useNativeHeader registers each framework's own middleware via UseNative, so
// the case can check a retained httpx.Context across a native layer. It takes
// the router rather than returning an httpx.Middleware because every httpx layer
// shares one native handler slot, and a native c.Next() would step past it.
func useNativeHeader(t *testing.T, framework string, router httpx.Router, key, value string) {
	t.Helper()
	switch framework {
	case "ginx":
		router.(*ginx.Router).UseNative(func(c *gin.Context) { c.Header(key, value) })
	case "fiberx":
		router.(*fiberx.Router).UseNative(func(c fiber.Ctx) error {
			c.Set(key, value)
			return c.Next()
		})
	case "echox":
		router.(*echox.Router).UseNative(func(next echo.HandlerFunc) echo.HandlerFunc {
			return func(c echo.Context) error {
				c.Response().Header().Set(key, value)
				return next(c)
			}
		})
	case "hertzx":
		router.(*hertzx.Router).UseNative(func(_ context.Context, rc *app.RequestContext) {
			rc.Header(key, value)
		})
	default:
		t.Fatalf("unknown framework %q", framework)
	}
}
