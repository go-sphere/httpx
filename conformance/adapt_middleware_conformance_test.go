package conformance

import (
	"context"
	"net/http"
	"net/http/httptest"
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

// nativeAdaptCase produces httpx.Middleware values by wrapping each adapter's
// native handler type via the corresponding Adapt*Middleware function. Each
// implementation follows that framework's idiomatic pass-through / terminate
// conventions so the observable outcome is the same.
type nativeAdaptCase struct {
	setHeader func(key, value string) httpx.Middleware
	blockWith func(status int, body string) httpx.Middleware
}

var nativeAdaptCases = map[string]nativeAdaptCase{
	"ginx": {
		setHeader: func(key, value string) httpx.Middleware {
			return ginx.AdaptGinMiddleware(func(c *gin.Context) {
				c.Header(key, value)
			})
		},
		blockWith: func(status int, body string) httpx.Middleware {
			return ginx.AdaptGinMiddleware(func(c *gin.Context) {
				c.String(status, body)
				c.Abort()
			})
		},
	},
	"fiberx": {
		setHeader: func(key, value string) httpx.Middleware {
			return fiberx.AdaptFiberMiddleware(func(c fiber.Ctx) error {
				c.Set(key, value)
				return c.Next()
			})
		},
		blockWith: func(status int, body string) httpx.Middleware {
			return fiberx.AdaptFiberMiddleware(func(c fiber.Ctx) error {
				return c.Status(status).SendString(body)
			})
		},
	},
	"echox": {
		setHeader: func(key, value string) httpx.Middleware {
			return echox.AdaptEchoMiddleware(func(next echo.HandlerFunc) echo.HandlerFunc {
				return func(c echo.Context) error {
					c.Response().Header().Set(key, value)
					return next(c)
				}
			})
		},
		blockWith: func(status int, body string) httpx.Middleware {
			return echox.AdaptEchoMiddleware(func(next echo.HandlerFunc) echo.HandlerFunc {
				return func(c echo.Context) error {
					return c.String(status, body)
				}
			})
		},
	},
	"hertzx": {
		setHeader: func(key, value string) httpx.Middleware {
			return hertzx.AdaptHertzMiddleware(func(_ context.Context, rc *app.RequestContext) {
				rc.Header(key, value)
			})
		},
		blockWith: func(status int, body string) httpx.Middleware {
			return hertzx.AdaptHertzMiddleware(func(_ context.Context, rc *app.RequestContext) {
				rc.String(status, body)
				rc.Abort()
			})
		},
	},
}

func runAdaptCaseAcrossFrameworks(
	t *testing.T,
	register func(t *testing.T, name string, r httpx.Router),
	request func() *http.Request,
) map[string]responseSnapshot {
	t.Helper()
	results := make(map[string]responseSnapshot, len(conformanceFrameworks))
	for _, name := range conformanceFrameworks {
		t.Logf("case=%s framework=%s", t.Name(), name)
		h := newHarness(t, name)
		register(t, name, h.Router)
		results[name] = h.Do(t, request())
	}
	return results
}

func TestAdaptNativeMiddlewareConformance(t *testing.T) {
	for _, name := range conformanceFrameworks {
		if _, ok := nativeAdaptCases[name]; !ok {
			t.Fatalf("missing native adapt case for framework %q", name)
		}
	}

	t.Run("PassThrough", func(t *testing.T) {
		results := runAdaptCaseAcrossFrameworks(t,
			func(t *testing.T, name string, r httpx.Router) {
				r.Use(nativeAdaptCases[name].setHeader("X-Trace", "from-native"))
				r.GET("/native/pass", func(ctx httpx.Context) error {
					return ctx.JSON(http.StatusOK, map[string]any{"ok": true})
				})
			},
			func() *http.Request {
				return httptest.NewRequest(http.MethodGet, "http://example.com/native/pass", nil)
			},
		)
		assertMatchesGin(t, results)
	})

	t.Run("NativeAbort", func(t *testing.T) {
		results := runAdaptCaseAcrossFrameworks(t,
			func(t *testing.T, name string, r httpx.Router) {
				r.Use(nativeAdaptCases[name].blockWith(http.StatusUnauthorized, "blocked"))
				r.GET("/native/blocked", func(ctx httpx.Context) error {
					return ctx.Text(http.StatusOK, "handler-should-not-run")
				})
			},
			func() *http.Request {
				return httptest.NewRequest(http.MethodGet, "http://example.com/native/blocked", nil)
			},
		)
		assertMatchesGin(t, results)
	})
}
