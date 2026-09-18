package httpxtest

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

func init() {
	register("Middleware", casesMiddleware)
	register("Router", casesRouter)
	register("Static", casesStatic)
}

func casesMiddleware(t *testing.T, r runner) {
	t.Run("PassThrough", func(t *testing.T) {
		r.assertGolden(t, func(router httpx.Router) {
			router.Use(func(ctx httpx.Context) error {
				ctx.Set("from-middleware", "ok")
				return ctx.Next()
			})
			router.GET("/mw/pass", func(ctx httpx.Context) error {
				v, _ := ctx.Get("from-middleware")
				return ctx.JSON(http.StatusOK, map[string]any{"value": v})
			})
		}, httptest.NewRequest(http.MethodGet, "http://example.com/mw/pass", nil))
	})

	t.Run("BeforeAfterNext", func(t *testing.T) {
		r.assertGolden(t, func(router httpx.Router) {
			router.Use(func(ctx httpx.Context) error {
				ctx.Set("order", []string{"before"})
				err := ctx.Next()
				appendOrder(ctx, "after")
				return err
			})
			router.GET("/mw/around", func(ctx httpx.Context) error {
				appendOrder(ctx, "handler")
				v, _ := ctx.Get("order")
				return ctx.JSON(http.StatusOK, map[string]any{"order": v})
			})
		}, httptest.NewRequest(http.MethodGet, "http://example.com/mw/around", nil))
	})

	t.Run("MiddlewareError", func(t *testing.T) {
		r.assertGolden(t, func(router httpx.Router) {
			router.Use(func(ctx httpx.Context) error {
				return errors.New("middleware boom")
			})
			router.GET("/mw/error", func(ctx httpx.Context) error {
				return ctx.Text(http.StatusOK, "handler-should-not-run")
			})
		}, httptest.NewRequest(http.MethodGet, "http://example.com/mw/error", nil))
	})

	t.Run("WriteWithoutNextStopsHandler", func(t *testing.T) {
		got := r.serve(t, func(router httpx.Router) {
			router.Use(func(ctx httpx.Context) error {
				return ctx.Text(http.StatusUnauthorized, "blocked")
			})
			router.GET("/mw/blocked", func(ctx httpx.Context) error {
				return ctx.Text(http.StatusOK, "handler-should-not-run")
			})
		}, httptest.NewRequest(http.MethodGet, "http://example.com/mw/blocked", nil))

		if got.Status != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d", got.Status, http.StatusUnauthorized)
		}
		if got.Body != "blocked" {
			t.Fatalf("body = %q, want %q", got.Body, "blocked")
		}
	})

	// Errors from several layers must all reach the middleware that drove the
	// chain, however the adapter aggregates them.
	t.Run("NextReturnsJoinedDownstreamErrors", func(t *testing.T) {
		errA := errors.New("err-a")
		errB := errors.New("err-b")
		var outerErr error

		got := r.serve(t, func(router httpx.Router) {
			router.Use(func(ctx httpx.Context) error {
				outerErr = ctx.Next()
				return outerErr
			})
			router.Use(func(ctx httpx.Context) error {
				err := ctx.Next()
				if err == nil {
					return errB
				}
				return errors.Join(err, errB)
			})
			router.GET("/mw/next/join-errors", func(ctx httpx.Context) error {
				return errA
			})
		}, httptest.NewRequest(http.MethodGet, "http://example.com/mw/next/join-errors", nil))

		if got.Status != http.StatusInternalServerError {
			t.Fatalf("status = %d, want %d", got.Status, http.StatusInternalServerError)
		}
		if outerErr == nil {
			t.Fatal("outer ctx.Next() returned nil")
		}
		if !errors.Is(outerErr, errA) {
			t.Fatalf("outer ctx.Next() = %v, want it to contain errA", outerErr)
		}
		if !errors.Is(outerErr, errB) {
			t.Fatalf("outer ctx.Next() = %v, want it to contain errB", outerErr)
		}
	})
}

func casesRouter(t *testing.T, r runner) {
	t.Run("BasePath", func(t *testing.T) {
		r.assertGolden(t, func(router httpx.Router) {
			g1 := router.Group("/api")
			g2 := g1.Group("/v1")
			g2.GET("/base", func(ctx httpx.Context) error {
				return ctx.JSON(http.StatusOK, map[string]any{
					"base1": g1.BasePath(),
					"base2": g2.BasePath(),
				})
			})
		}, httptest.NewRequest(http.MethodGet, "http://example.com/api/v1/base", nil))
	})

	t.Run("Handle", func(t *testing.T) {
		r.assertGolden(t, func(router httpx.Router) {
			router.Handle(http.MethodPut, "/api/handle", func(ctx httpx.Context) error {
				return ctx.Text(http.StatusOK, "handle-put")
			})
		}, httptest.NewRequest(http.MethodPut, "http://example.com/api/handle", nil))
	})

	t.Run("HTTPShortcuts", func(t *testing.T) {
		shortcuts := []struct {
			name     string
			method   string
			register func(httpx.Router)
		}{
			{"PUT", http.MethodPut, func(router httpx.Router) {
				router.PUT("/api/put", func(ctx httpx.Context) error { return ctx.Text(http.StatusOK, "put") })
			}},
			{"DELETE", http.MethodDelete, func(router httpx.Router) {
				router.DELETE("/api/delete", func(ctx httpx.Context) error { return ctx.Text(http.StatusOK, "delete") })
			}},
			{"PATCH", http.MethodPatch, func(router httpx.Router) {
				router.PATCH("/api/patch", func(ctx httpx.Context) error { return ctx.Text(http.StatusOK, "patch") })
			}},
			{"HEAD", http.MethodHead, func(router httpx.Router) {
				router.HEAD("/api/head", func(ctx httpx.Context) error { return ctx.NoContent(http.StatusNoContent) })
			}},
			{"OPTIONS", http.MethodOptions, func(router httpx.Router) {
				router.OPTIONS("/api/options", func(ctx httpx.Context) error { return ctx.Text(http.StatusOK, "options") })
			}},
		}
		for _, tc := range shortcuts {
			t.Run(tc.name, func(t *testing.T) {
				req := httptest.NewRequest(tc.method, "http://example.com/api/"+strings.ToLower(tc.name), nil)
				r.assertGolden(t, tc.register, req)
			})
		}
	})

	// Engine, group and route middleware must nest in registration order
	// around the handler.
	t.Run("GroupUseAnyAndNext", func(t *testing.T) {
		r.assertGolden(t, func(router httpx.Router) {
			router.Use(func(ctx httpx.Context) error {
				ctx.Set("order", []string{"global-before"})
				err := ctx.Next()
				appendOrder(ctx, "global-after")
				return err
			})
			g := router.Group("/api", func(ctx httpx.Context) error {
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
				return ctx.JSON(http.StatusOK, map[string]any{"order": v})
			})
		}, httptest.NewRequest(http.MethodPost, "http://example.com/api/ping", nil))
	})
}

func casesStatic(t *testing.T, r runner) {
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "hello.txt"), []byte("static-content"), 0o600); err != nil {
		t.Fatalf("write static file: %v", err)
	}

	t.Run("Static", func(t *testing.T) {
		r.assertGolden(t, func(router httpx.Router) {
			router.Static("/assets", tmp)
		}, httptest.NewRequest(http.MethodGet, "http://example.com/assets/hello.txt", nil))
	})

	t.Run("StaticFS", func(t *testing.T) {
		r.assertGolden(t, func(router httpx.Router) {
			router.StaticFS("/files", os.DirFS(tmp))
		}, httptest.NewRequest(http.MethodGet, "http://example.com/files/hello.txt", nil))
	})

	// A directory is served by its index.html, which is how a single-page app
	// mounted on a prefix is served. This was the one static behavior the four
	// adapters did not agree on before: gin and echo served the index, hertz
	// returned 404.
	t.Run("StaticDirectoryServesIndex", func(t *testing.T) {
		withIndex := t.TempDir()
		if err := os.WriteFile(filepath.Join(withIndex, "index.html"), []byte("<h1>index</h1>"), 0o600); err != nil {
			t.Fatalf("write index: %v", err)
		}
		r.assertGolden(t, func(router httpx.Router) {
			router.Static("/app", withIndex)
		}, httptest.NewRequest(http.MethodGet, "http://example.com/app/", nil))
	})

	// Without an index.html a directory is 404, never a listing.
	t.Run("StaticDirectoryWithoutIndexIs404", func(t *testing.T) {
		got := r.serve(t, func(router httpx.Router) {
			router.Static("/assets", tmp)
		}, httptest.NewRequest(http.MethodGet, "http://example.com/assets/", nil))

		if got.Status != http.StatusNotFound {
			t.Fatalf("status = %d, want 404 (no listing); body=%q", got.Status, got.Body)
		}
		if strings.Contains(got.Body, "hello.txt") {
			t.Fatalf("the response listed the directory: %q", got.Body)
		}
	})
}

// appendOrder records execution order in request state, which is how the
// middleware cases observe nesting without depending on response writing.
func appendOrder(ctx httpx.Context, s string) {
	v, _ := ctx.Get("order")
	arr, _ := v.([]string)
	ctx.Set("order", append(arr, s))
}

func init() {
	register("StaticInterceptors", casesStaticInterceptors)
}

// Static mounts and mounted net/http handlers are ordinary routes, so an
// interceptor registered on their scope wraps them like any other handler.
// This is what lets an access log or an authorization layer cover a static
// asset mount.
func casesStaticInterceptors(t *testing.T, r runner) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("static-content"), 0o600); err != nil {
		t.Fatalf("write static file: %v", err)
	}

	for _, tc := range []struct {
		name     string
		register func(httpx.Router, httpx.Interceptor)
		path     string
		want     string
	}{
		{
			name: "Static",
			register: func(router httpx.Router, i httpx.Interceptor) {
				httpx.UseInterceptor(router, i)
				router.Static("/assets", dir)
			},
			path: "/assets/hello.txt",
			want: "static-content",
		},
		{
			name: "StaticFS",
			register: func(router httpx.Router, i httpx.Interceptor) {
				httpx.UseInterceptor(router, i)
				router.StaticFS("/files", os.DirFS(dir))
			},
			path: "/files/hello.txt",
			want: "static-content",
		},
		{
			name: "HandleStd",
			register: func(router httpx.Router, i httpx.Interceptor) {
				httpx.UseInterceptor(router, i)
				httpx.MountStd(router, http.MethodGet, "/mounted", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					_, _ = w.Write([]byte("from-std"))
				}))
			},
			path: "/mounted",
			want: "from-std",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var ran bool
			got := r.serve(t, func(router httpx.Router) {
				tc.register(router, func(next httpx.Handler) httpx.Handler {
					return func(ctx httpx.Context) error {
						ran = true
						ctx.SetHeader("X-Interceptor", "1")
						return next(ctx)
					}
				})
			}, httptest.NewRequest(http.MethodGet, "http://example.com"+tc.path, nil))

			if got.Status != http.StatusOK {
				t.Fatalf("status = %d, want 200; body=%q", got.Status, got.Body)
			}
			if got.Body != tc.want {
				t.Fatalf("body = %q, want %q", got.Body, tc.want)
			}
			if !ran {
				t.Fatal("the interceptor did not run for this mount")
			}
			if got.Headers.Get("X-Interceptor") != "1" {
				t.Fatal("the interceptor's header did not reach the response")
			}
		})
	}

	// An interceptor that stops the chain must also stop a static mount —
	// that is the point of covering it (an authorization layer in front of
	// private assets).
	t.Run("InterceptorCanBlockStatic", func(t *testing.T) {
		got := r.serve(t, func(router httpx.Router) {
			httpx.UseInterceptor(router, func(next httpx.Handler) httpx.Handler {
				return func(ctx httpx.Context) error {
					return ctx.Text(http.StatusForbidden, "denied")
				}
			})
			router.Static("/private", dir)
		}, httptest.NewRequest(http.MethodGet, "http://example.com/private/hello.txt", nil))

		if got.Status != http.StatusForbidden || strings.TrimSpace(got.Body) != "denied" {
			t.Fatalf("response = %d %q, want 403 %q", got.Status, got.Body, "denied")
		}
	})
}
