package httpxtest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-sphere/httpx"
)

func init() {
	register("Response", casesResponse)
	register("Context", casesContext)
}

// Every way a handler can write a response, recorded as a contract.
func casesResponse(t *testing.T, r runner) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "hello.txt")
	if err := os.WriteFile(filePath, []byte("from-file"), 0o600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	cases := []struct {
		name     string
		register func(httpx.Router)
		path     string
	}{
		{"Status", func(router httpx.Router) {
			router.GET("/status", func(ctx httpx.Context) error {
				ctx.Status(http.StatusAccepted)
				return nil
			})
		}, "/status"},
		{"StatusThenJSON", func(router httpx.Router) {
			router.GET("/status-json", func(ctx httpx.Context) error {
				ctx.Status(http.StatusAccepted)
				return ctx.JSON(http.StatusCreated, map[string]any{"ok": true})
			})
		}, "/status-json"},
		{"JSON", func(router httpx.Router) {
			router.GET("/json", func(ctx httpx.Context) error {
				return ctx.JSON(http.StatusCreated, map[string]any{"ok": true})
			})
		}, "/json"},
		{"Text", func(router httpx.Router) {
			router.GET("/text", func(ctx httpx.Context) error {
				return ctx.Text(http.StatusAccepted, "hello")
			})
		}, "/text"},
		{"NoContent", func(router httpx.Router) {
			router.GET("/nocontent", func(ctx httpx.Context) error {
				return ctx.NoContent(http.StatusNoContent)
			})
		}, "/nocontent"},
		{"Bytes", func(router httpx.Router) {
			router.GET("/bytes", func(ctx httpx.Context) error {
				return ctx.Bytes(http.StatusOK, []byte("abc"), "application/octet-stream")
			})
		}, "/bytes"},
		{"DataFromReader", func(router httpx.Router) {
			router.GET("/reader", func(ctx httpx.Context) error {
				// Declared int64 rather than written as an untyped constant on
				// purpose: an untyped 6 satisfies both int and int64, so it
				// would keep compiling if the parameter were narrowed back to
				// int, whereas this makes the suite fail to build.
				var size int64 = 6
				return ctx.DataFromReader(http.StatusOK, "text/plain", strings.NewReader("stream"), size)
			})
		}, "/reader"},
		{"File", func(router httpx.Router) {
			router.GET("/file", func(ctx httpx.Context) error {
				return ctx.File(filePath)
			})
		}, "/file"},
		{"Redirect", func(router httpx.Router) {
			router.GET("/redirect", func(ctx httpx.Context) error {
				return ctx.Redirect(http.StatusFound, "/to")
			})
		}, "/redirect"},
		{"HeaderAndCookie", func(router httpx.Router) {
			router.GET("/cookie", func(ctx httpx.Context) error {
				ctx.SetHeader("X-Trace", "ok")
				ctx.SetCookie(&http.Cookie{Name: "session", Value: "abc", Path: "/"})
				return ctx.Text(http.StatusOK, "cookie")
			})
		}, "/cookie"},
		{"ErrorPropagation", func(router httpx.Router) {
			router.GET("/api/err", func(ctx httpx.Context) error {
				return errors.New("boom")
			})
		}, "/api/err"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r.assertGolden(t, tc.register, httptest.NewRequest(http.MethodGet, "http://example.com"+tc.path, nil))
		})
	}

	// An unknown size must not become an illegal "Content-Length: -1".
	t.Run("DataFromReaderUnknownSize", func(t *testing.T) {
		got := r.serve(t, func(router httpx.Router) {
			router.GET("/reader-unknown", func(ctx httpx.Context) error {
				return ctx.DataFromReader(http.StatusOK, "text/plain", strings.NewReader("stream"), -1)
			})
		}, httptest.NewRequest(http.MethodGet, "http://example.com/reader-unknown", nil))

		if cl := got.Headers.Get("Content-Length"); cl == "-1" {
			t.Fatal("emitted an illegal Content-Length: -1")
		}
		r.compareGolden(t, got)
	})

	casesWithJSON(t, r)
}

// The success-envelope wrapper generated services use lives downstream
// (sphere/server/httpz.WithJson), not here — an envelope is a convention, not
// part of the transport contract. What every adapter still owes such a wrapper
// is pinned by hand so the cases do not depend on which package defines the
// envelope: a recovered panic and a returned error both travel the ordinary
// Handler error path to the configured error handler with their text intact.
func casesWithJSON(t *testing.T, r runner) {
	// recovered is the wrapper's panic path: the panic value becomes an httpx
	// 500 returned to the adapter, never a framework-rendered stack trace.
	recovered := func(h httpx.Handler) httpx.Handler {
		return func(ctx httpx.Context) (err error) {
			defer func() {
				if rec := recover(); rec != nil {
					err = httpx.InternalServerError(fmt.Errorf("%v", rec))
				}
			}()
			return h(ctx)
		}
	}

	// A recovered panic and a returned error must both reach the configured error
	// handler with the original message.
	t.Run("WithJSONPanicThroughCustomErrorHandler", func(t *testing.T) {
		got := r.serveWith(t, Options{ErrorHandler: teapotErrorHandler}, func(router httpx.Router) {
			router.GET("/withjson/panic/custom", recovered(func(ctx httpx.Context) error {
				panic("boom")
			}))
		}, httptest.NewRequest(http.MethodGet, "http://example.com/withjson/panic/custom", nil))

		if got.Status != http.StatusTeapot {
			t.Fatalf("status = %d, want %d; body=%q", got.Status, http.StatusTeapot, got.Body)
		}
		var payload map[string]string
		if err := json.Unmarshal([]byte(got.Body), &payload); err != nil {
			t.Fatalf("parse body: %v; body=%q", err, got.Body)
		}
		if payload["error"] != "boom" {
			t.Fatalf("error = %q, want %q", payload["error"], "boom")
		}
	})

	t.Run("WithJSONErrorThroughCustomErrorHandler", func(t *testing.T) {
		got := r.serveWith(t, Options{ErrorHandler: teapotErrorHandler}, func(router httpx.Router) {
			router.GET("/withjson/err/custom", recovered(func(ctx httpx.Context) error {
				return errors.New("boom")
			}))
		}, httptest.NewRequest(http.MethodGet, "http://example.com/withjson/err/custom", nil))

		if got.Status != http.StatusTeapot {
			t.Fatalf("status = %d, want %d; body=%q", got.Status, http.StatusTeapot, got.Body)
		}
	})

	// File serves regular files only. A directory or a missing path is a 404
	// rendered by the error handler: never a listing, a redirect to the
	// directory's server path, or a body naming it.
	t.Run("FileRefusesDirectoryAndMissingPath", func(t *testing.T) {
		dir := t.TempDir()
		sub := filepath.Join(dir, "sub")
		if err := os.Mkdir(sub, 0o700); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(sub, "index.html"), []byte("index"), 0o600); err != nil {
			t.Fatalf("write index: %v", err)
		}
		for _, tc := range []struct{ name, path, target string }{
			{"Directory", sub, "/file/dir"},
			{"DirectoryTrailingSlash", sub, "/file/dir/"},
			{"Missing", filepath.Join(dir, "missing.txt"), "/file/missing"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				got := r.serveWith(t, Options{ErrorHandler: statusTextErrorHandler}, func(router httpx.Router) {
					router.GET(tc.target, func(ctx httpx.Context) error {
						return ctx.File(tc.path)
					})
				}, httptest.NewRequest(http.MethodGet, "http://example.com"+tc.target, nil))
				if got.Status != http.StatusNotFound || got.Body != "err:404" {
					t.Fatalf("status=%d body=%q, want 404 %q", got.Status, got.Body, "err:404")
				}
				if loc := got.Headers.Get("Location"); loc != "" {
					t.Fatalf("Location = %q, want none", loc)
				}
			})
		}
	})

	// A file that exists but cannot be opened is a 403 from the route, not an
	// unmatched path: the engine's layers run once and see the route.
	t.Run("FileUnreadableIsForbidden", func(t *testing.T) {
		if os.Geteuid() == 0 {
			r.skipf(t, "file permissions do not apply to root")
		}
		path := filepath.Join(t.TempDir(), "secret.txt")
		if err := os.WriteFile(path, []byte("secret"), 0o000); err != nil {
			t.Fatalf("write file: %v", err)
		}
		var seen []string
		got := r.serveEngine(t, Options{ErrorHandler: statusTextErrorHandler}, func(engine httpx.Engine) {
			engine.Use(func(next httpx.Handler) httpx.Handler {
				return func(ctx httpx.Context) error {
					seen = append(seen, ctx.FullPath())
					return next(ctx)
				}
			})
			engine.Group("").GET("/file/secret", func(ctx httpx.Context) error {
				return ctx.File(path)
			})
		}, httptest.NewRequest(http.MethodGet, "http://example.com/file/secret", nil))
		if got.Status != http.StatusForbidden || got.Body != "err:403" {
			t.Fatalf("status=%d body=%q, want 403 %q", got.Status, got.Body, "err:403")
		}
		if len(seen) != 1 || seen[0] != "/file/secret" {
			t.Fatalf("engine layer saw %q, want one run on %q", seen, "/file/secret")
		}
	})

	// 1xx, 204 and 304 carry no body: a write with one commits the status and
	// drops the bytes, without an error.
	t.Run("BodylessStatusDropsBody", func(t *testing.T) {
		for _, tc := range []struct {
			name  string
			write func(httpx.Context) error
		}{
			{"Text", func(ctx httpx.Context) error { return ctx.Text(http.StatusNoContent, "dropped") }},
			{"Bytes", func(ctx httpx.Context) error { return ctx.Bytes(http.StatusNoContent, []byte("dropped"), "text/plain") }},
			{"JSON", func(ctx httpx.Context) error { return ctx.JSON(http.StatusNoContent, "dropped") }},
			{"DataFromReader", func(ctx httpx.Context) error {
				return ctx.DataFromReader(http.StatusNoContent, "text/plain", strings.NewReader("dropped"), 7)
			}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				var writeErr error
				got := r.serve(t, func(router httpx.Router) {
					router.GET("/bodyless", func(ctx httpx.Context) error {
						writeErr = tc.write(ctx)
						return writeErr
					})
				}, httptest.NewRequest(http.MethodGet, "http://example.com/bodyless", nil))
				if writeErr != nil {
					t.Fatalf("write returned %v, want nil", writeErr)
				}
				if got.Status != http.StatusNoContent || got.Body != "" {
					t.Fatalf("status=%d body=%q, want 204 and no body", got.Status, got.Body)
				}
			})
		}
	})

	// File leaves the request as it was: a layer reading Path or the query
	// after the handler sees the request URL, not the file's path, and a file
	// name holding URL syntax is served like any other.
	t.Run("FileKeepsRequestURL", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "a #?%41 b.txt")
		if err := os.WriteFile(path, []byte("special"), 0o600); err != nil {
			t.Fatalf("write file: %v", err)
		}
		var after string
		got := r.serve(t, func(router httpx.Router) {
			router.Use(func(next httpx.Handler) httpx.Handler {
				return func(ctx httpx.Context) error {
					err := next(ctx)
					after = ctx.Path() + "?" + ctx.RawQuery()
					return err
				}
			})
			router.GET("/file/special", func(ctx httpx.Context) error {
				return ctx.File(path)
			})
		}, httptest.NewRequest(http.MethodGet, "http://example.com/file/special?q=1", nil))
		if got.Status != http.StatusOK || got.Body != "special" {
			t.Fatalf("status=%d body=%q, want 200 %q", got.Status, got.Body, "special")
		}
		if after != "/file/special?q=1" {
			t.Fatalf("after File the layer saw %q, want %q", after, "/file/special?q=1")
		}
	})

	// SetHeader with an empty value sends the header empty, as
	// http.Header.Set does; it does not delete it.
	t.Run("SetHeaderEmptyValueIsSent", func(t *testing.T) {
		got := r.serve(t, func(router httpx.Router) {
			router.GET("/header/empty", func(ctx httpx.Context) error {
				ctx.SetHeader("X-Empty", "")
				return ctx.Text(http.StatusOK, "ok")
			})
		}, httptest.NewRequest(http.MethodGet, "http://example.com/header/empty", nil))
		if values, ok := got.Headers["X-Empty"]; !ok || len(values) != 1 || values[0] != "" {
			t.Fatalf("X-Empty = %q (present %v), want one empty value", values, ok)
		}
	})

	// Every code ValidRedirectCode accepts is sent as given.
	t.Run("RedirectKeepsEveryValidCode", func(t *testing.T) {
		for _, code := range []int{
			http.StatusMultipleChoices, http.StatusMovedPermanently, http.StatusFound,
			http.StatusSeeOther, http.StatusTemporaryRedirect, http.StatusPermanentRedirect,
		} {
			got := r.serve(t, func(router httpx.Router) {
				router.GET("/redirect/valid", func(ctx httpx.Context) error {
					return ctx.Redirect(code, "/to")
				})
			}, httptest.NewRequest(http.MethodGet, "http://example.com/redirect/valid", nil))
			if got.Status != code || got.Headers.Get("Location") != "/to" {
				t.Fatalf("Redirect(%d): status=%d Location=%q, want %d %q", code, got.Status, got.Headers.Get("Location"), code, "/to")
			}
		}
	})

	// The request URL plays no part in File: a URL ending in /index.html
	// serves the file rather than redirecting to "./".
	t.Run("FileIgnoresIndexHTMLRequestPath", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "page.html")
		if err := os.WriteFile(path, []byte("page"), 0o600); err != nil {
			t.Fatalf("write file: %v", err)
		}
		got := r.serve(t, func(router httpx.Router) {
			router.GET("/site/index.html", func(ctx httpx.Context) error {
				return ctx.File(path)
			})
		}, httptest.NewRequest(http.MethodGet, "http://example.com/site/index.html", nil))
		if got.Status != http.StatusOK || got.Body != "page" {
			t.Fatalf("status=%d body=%q, want 200 %q", got.Status, got.Body, "page")
		}
		if loc := got.Headers.Get("Location"); loc != "" {
			t.Fatalf("Location = %q, want none", loc)
		}
	})

	// ResponseHeaderEditor appends where SetHeader replaces, reads back what is
	// set, and like every header write is dropped once the response commits.
	t.Run("ResponseHeaderEditorAppends", func(t *testing.T) {
		probed := true
		got := r.serve(t, func(router httpx.Router) {
			router.GET("/headers/add", func(ctx httpx.Context) error {
				editor, ok := httpx.AsResponseHeaderEditor(ctx)
				if !ok {
					probed = false
					return nil
				}
				ctx.SetHeader("Vary", "Accept-Encoding")
				editor.AddHeader("Vary", "Origin")
				vary := editor.ResponseHeaderValues("vary")
				unset := editor.ResponseHeaderValues("X-Unset")
				if err := ctx.Text(http.StatusOK, fmt.Sprintf("%s|%d", strings.Join(vary, ","), len(unset))); err != nil {
					return err
				}
				editor.AddHeader("X-Late", "1")
				return nil
			})
		}, httptest.NewRequest(http.MethodGet, "http://example.com/headers/add", nil))
		if !probed {
			r.skipf(t, "%s: Context does not implement httpx.ResponseHeaderEditor", r.suite.Name)
		}
		if want := "Accept-Encoding,Origin|0"; got.Status != http.StatusOK || got.Body != want {
			t.Fatalf("status=%d body=%q, want 200 %q", got.Status, got.Body, want)
		}
		if vary := got.Headers.Values("Vary"); strings.Join(vary, ",") != "Accept-Encoding,Origin" {
			t.Fatalf("Vary = %q, want [Accept-Encoding Origin]", vary)
		}
		if late := got.Headers.Get("X-Late"); late != "" {
			t.Fatalf("X-Late = %q, want none: AddHeader after commit must be dropped", late)
		}
	})
}

// The standard context.Context behind a request, and its separation from the
// request-scoped state store.
func casesContext(t *testing.T, r runner) {
	type ctxKey struct{}

	t.Run("StandardContext", func(t *testing.T) {
		r.assertGolden(t, func(router httpx.Router) {
			router.GET("/ctx/base", func(ctx httpx.Context) error {
				ctx.Set("trace-id", "trace-1")
				v, _ := ctx.Get("trace-id")
				trace, _ := v.(string)
				return ctx.JSON(http.StatusOK, map[string]any{
					"errIsNil": ctx.Context().Err() == nil,
					"value":    trace,
				})
			})
		}, httptest.NewRequest(http.MethodGet, "http://example.com/ctx/base", nil))
	})

	// A value injected by middleware through SetContext must reach the handler.
	t.Run("SetContextAndRetrieve", func(t *testing.T) {
		r.assertGolden(t, func(router httpx.Router) {
			router.Use(func(next httpx.Handler) httpx.Handler {
				return func(ctx httpx.Context) error {
					ctx.SetContext(context.WithValue(ctx.Context(), ctxKey{}, "injected-value"))
					return next(ctx)
				}
			})
			router.GET("/ctx/setctx", func(ctx httpx.Context) error {
				val, _ := ctx.Context().Value(ctxKey{}).(string)
				return ctx.JSON(http.StatusOK, map[string]any{"value": val})
			})
		}, httptest.NewRequest(http.MethodGet, "http://example.com/ctx/setctx", nil))
	})

	// The state store is not the context: Set is not visible through
	// Context().Value, which is why middleware must use SetContext to pass values
	// into downstream calls.
	t.Run("StateStoreAndContextAreSeparate", func(t *testing.T) {
		r.assertGolden(t, func(router httpx.Router) {
			router.GET("/ctx/separate", func(ctx httpx.Context) error {
				ctx.Set("store-key", "store-value")
				ctx.SetContext(context.WithValue(ctx.Context(), ctxKey{}, "ctx-value"))
				storeVal, storeOk := ctx.Get("store-key")
				storeStr, _ := storeVal.(string)
				ctxVal, _ := ctx.Context().Value(ctxKey{}).(string)
				_, storeThroughContext := ctx.Context().Value("store-key").(string)
				return ctx.JSON(http.StatusOK, map[string]any{
					"storeOk":             storeOk,
					"storeVal":            storeStr,
					"ctxVal":              ctxVal,
					"storeThroughContext": storeThroughContext,
				})
			})
		}, httptest.NewRequest(http.MethodGet, "http://example.com/ctx/separate", nil))
	})
}
