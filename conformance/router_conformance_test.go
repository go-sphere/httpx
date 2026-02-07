package conformance

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-sphere/httpx"
)

func TestRouterConformance(t *testing.T) {
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
