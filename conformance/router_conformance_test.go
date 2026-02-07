package conformance

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-sphere/httpx"
)

func TestMiddlewareStyleConformance(t *testing.T) {
	t.Run("PassThrough", func(t *testing.T) {
		results := runAcrossFrameworks(t, func(r httpx.Router) {
			r.Use(func(ctx httpx.Context) error {
				ctx.Set("from-middleware", "ok")
				return ctx.Next()
			})
			r.GET("/mw/pass", func(ctx httpx.Context) error {
				v, _ := ctx.Get("from-middleware")
				return ctx.JSON(200, map[string]any{"value": v})
			})
		}, func() *http.Request {
			return httptest.NewRequest(http.MethodGet, "http://example.com/mw/pass", nil)
		})
		assertMatchesGin(t, results)
	})

	t.Run("BeforeAfterNext", func(t *testing.T) {
		results := runAcrossFrameworks(t, func(r httpx.Router) {
			appendOrder := func(ctx httpx.Context, s string) {
				v, _ := ctx.Get("order")
				arr, _ := v.([]string)
				arr = append(arr, s)
				ctx.Set("order", arr)
			}

			r.Use(func(ctx httpx.Context) error {
				ctx.Set("order", []string{"before"})
				err := ctx.Next()
				appendOrder(ctx, "after")
				return err
			})

			r.GET("/mw/around", func(ctx httpx.Context) error {
				appendOrder(ctx, "handler")
				v, _ := ctx.Get("order")
				return ctx.JSON(200, map[string]any{"order": v})
			})
		}, func() *http.Request {
			return httptest.NewRequest(http.MethodGet, "http://example.com/mw/around", nil)
		})
		assertMatchesGin(t, results)
	})

	t.Run("MiddlewareError", func(t *testing.T) {
		results := runAcrossFrameworks(t, func(r httpx.Router) {
			r.Use(func(ctx httpx.Context) error {
				return errors.New("middleware boom")
			})
			r.GET("/mw/error", func(ctx httpx.Context) error {
				return ctx.Text(200, "handler-should-not-run")
			})
		}, func() *http.Request {
			return httptest.NewRequest(http.MethodGet, "http://example.com/mw/error", nil)
		})
		assertMatchesGin(t, results)
	})
}

func TestRouterConformance(t *testing.T) {
	t.Run("BasePath", func(t *testing.T) {
		results := runAcrossFrameworks(t, func(r httpx.Router) {
			g1 := r.Group("/api")
			g2 := g1.Group("/v1")
			g2.GET("/base", func(ctx httpx.Context) error {
				return ctx.JSON(200, map[string]any{
					"base1": g1.BasePath(),
					"base2": g2.BasePath(),
				})
			})
		}, func() *http.Request {
			return httptest.NewRequest(http.MethodGet, "http://example.com/api/v1/base", nil)
		})
		assertMatchesGin(t, results)
	})

	t.Run("Handle", func(t *testing.T) {
		results := runAcrossFrameworks(t, func(r httpx.Router) {
			r.Handle(http.MethodPut, "/api/handle", func(ctx httpx.Context) error {
				return ctx.Text(200, "handle-put")
			})
		}, func() *http.Request {
			return httptest.NewRequest(http.MethodPut, "http://example.com/api/handle", nil)
		})
		assertMatchesGin(t, results)
	})

	t.Run("HTTPShortcuts", func(t *testing.T) {
		tests := []struct {
			name     string
			method   string
			register func(httpx.Router)
		}{
			{
				name:   "PUT",
				method: http.MethodPut,
				register: func(r httpx.Router) {
					r.PUT("/api/put", func(ctx httpx.Context) error { return ctx.Text(200, "put") })
				},
			},
			{
				name:   "DELETE",
				method: http.MethodDelete,
				register: func(r httpx.Router) {
					r.DELETE("/api/delete", func(ctx httpx.Context) error { return ctx.Text(200, "delete") })
				},
			},
			{
				name:   "PATCH",
				method: http.MethodPatch,
				register: func(r httpx.Router) {
					r.PATCH("/api/patch", func(ctx httpx.Context) error { return ctx.Text(200, "patch") })
				},
			},
			{
				name:   "HEAD",
				method: http.MethodHead,
				register: func(r httpx.Router) {
					r.HEAD("/api/head", func(ctx httpx.Context) error { return ctx.NoContent(204) })
				},
			},
			{
				name:   "OPTIONS",
				method: http.MethodOptions,
				register: func(r httpx.Router) {
					r.OPTIONS("/api/options", func(ctx httpx.Context) error { return ctx.Text(200, "options") })
				},
			},
		}

		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				results := runAcrossFrameworks(t, tc.register, func() *http.Request {
					return httptest.NewRequest(tc.method, "http://example.com/api/"+strings.ToLower(tc.name), nil)
				})
				assertMatchesGin(t, results)
			})
		}
	})

	t.Run("GroupUseAnyAndNext", func(t *testing.T) {
		results := runAcrossFrameworks(t, func(r httpx.Router) {
			appendOrder := func(ctx httpx.Context, s string) {
				v, _ := ctx.Get("order")
				arr, _ := v.([]string)
				arr = append(arr, s)
				ctx.Set("order", arr)
			}

			r.Use(func(ctx httpx.Context) error {
				ctx.Set("order", []string{"global-before"})
				err := ctx.Next()
				appendOrder(ctx, "global-after")
				return err
			})

			g := r.Group("/api", func(ctx httpx.Context) error {
				appendOrder(ctx, "group-before")
				err := ctx.Next()
				appendOrder(ctx, "group-after")
				return err
			})

			g.Use(func(ctx httpx.Context) error {
				appendOrder(ctx, "route-before")
				err := ctx.Next()
				appendOrder(ctx, "route-after")
				return err
			})

			g.Any("/ping", func(ctx httpx.Context) error {
				appendOrder(ctx, "handler")
				v, _ := ctx.Get("order")
				return ctx.JSON(200, map[string]any{"order": v})
			})
		}, func() *http.Request {
			return httptest.NewRequest(http.MethodPost, "http://example.com/api/ping", nil)
		})
		assertMatchesGin(t, results)
	})
}

func TestStaticConformance(t *testing.T) {
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "hello.txt"), []byte("static-content"), 0o600); err != nil {
		t.Fatalf("write static file: %v", err)
	}

	t.Run("Static", func(t *testing.T) {
		results := runAcrossFrameworks(t, func(r httpx.Router) {
			r.Static("/assets", tmp)
		}, func() *http.Request {
			return httptest.NewRequest(http.MethodGet, "http://example.com/assets/hello.txt", nil)
		})
		assertMatchesGin(t, results)
	})

	t.Run("StaticFS", func(t *testing.T) {
		results := runAcrossFrameworks(t, func(r httpx.Router) {
			r.StaticFS("/files", os.DirFS(tmp))
		}, func() *http.Request {
			return httptest.NewRequest(http.MethodGet, "http://example.com/files/hello.txt", nil)
		})
		assertMatchesGin(t, results)
	})
}
