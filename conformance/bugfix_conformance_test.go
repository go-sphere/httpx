package conformance

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-sphere/httpx"
)

func writeStaticFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		t.Fatalf("write static file: %v", err)
	}
}

// TestNamedWildcardParamConformance covers B3/B4/A2: a generated route like
// /files/*filepath must register and resolve on every adapter, and the
// wildcard value must not carry a leading slash.
func TestNamedWildcardParamConformance(t *testing.T) {
	for _, name := range conformanceFrameworks {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, name)
			h.Router.Handle("GET", "/files/*filepath", func(ctx httpx.Context) error {
				return ctx.JSON(http.StatusOK, map[string]any{
					"param":  ctx.Param("filepath"),
					"params": ctx.Params(),
				})
			})

			got := h.Do(t, httptest.NewRequest(http.MethodGet, "http://example.com/files/a/b.txt", nil))
			if got.Status != http.StatusOK {
				t.Fatalf("%s status = %d, want 200; body=%q", name, got.Status, got.Body)
			}
			var payload struct {
				Param  string            `json:"param"`
				Params map[string]string `json:"params"`
			}
			if err := json.Unmarshal([]byte(got.Body), &payload); err != nil {
				t.Fatalf("%s parse body: %v; body=%q", name, err, got.Body)
			}
			if payload.Param != "a/b.txt" {
				t.Fatalf("%s Param(filepath) = %q, want %q", name, payload.Param, "a/b.txt")
			}
			if payload.Params["filepath"] != "a/b.txt" {
				t.Fatalf("%s Params()[filepath] = %q, want %q (params=%v)", name, payload.Params["filepath"], "a/b.txt", payload.Params)
			}
		})
	}
}

// TestBodyRawThenBindJSONConformance covers A3: reading the raw body must not
// consume it for a subsequent BindJSON on any adapter.
func TestBodyRawThenBindJSONConformance(t *testing.T) {
	results := runAcrossFrameworks(t, func(r httpx.Router) {
		r.POST("/body/reread", func(ctx httpx.Context) error {
			raw, err := ctx.BodyRaw()
			if err != nil {
				return err
			}
			var dst struct {
				Name string `json:"name"`
			}
			if err := ctx.BindJSON(&dst); err != nil {
				return err
			}
			return ctx.JSON(http.StatusOK, map[string]any{
				"rawLen": len(raw),
				"name":   dst.Name,
			})
		})
	}, func() *http.Request {
		req := httptest.NewRequest(http.MethodPost, "http://example.com/body/reread", bytes.NewBufferString(`{"name":"sphere"}`))
		req.Header.Set("Content-Type", "application/json")
		return req
	})
	assertMatchesGin(t, results)
	var payload struct {
		RawLen int    `json:"rawLen"`
		Name   string `json:"name"`
	}
	if err := json.Unmarshal([]byte(results["ginx"].Body), &payload); err != nil {
		t.Fatalf("parse gin body: %v", err)
	}
	if payload.Name != "sphere" || payload.RawLen == 0 {
		t.Fatalf("gin baseline itself broken: %+v", payload)
	}
}

// TestHeadersExcludeHostConformance covers A5: Headers() must not leak the
// Host pseudo-header on any adapter.
func TestHeadersExcludeHostConformance(t *testing.T) {
	for _, name := range conformanceFrameworks {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, name)
			h.Router.GET("/headers/nohost", func(ctx httpx.Context) error {
				headers := ctx.Headers()
				_, hasHost := headers["Host"]
				return ctx.JSON(http.StatusOK, map[string]any{
					"hasHost": hasHost,
					"trace":   ctx.Header("X-Trace-In"),
				})
			})

			req := httptest.NewRequest(http.MethodGet, "http://example.com/headers/nohost", nil)
			req.Header.Set("X-Trace-In", "t1")
			got := h.Do(t, req)
			var payload struct {
				HasHost bool   `json:"hasHost"`
				Trace   string `json:"trace"`
			}
			if err := json.Unmarshal([]byte(got.Body), &payload); err != nil {
				t.Fatalf("%s parse body: %v; body=%q", name, err, got.Body)
			}
			if payload.HasHost {
				t.Fatalf("%s Headers() leaked Host", name)
			}
			if payload.Trace != "t1" {
				t.Fatalf("%s Header(X-Trace-In) = %q, want %q", name, payload.Trace, "t1")
			}
		})
	}
}

// TestSetCookieFullFidelityConformance covers A6: cookies with Expires and a
// space in the value must serialize identically on every adapter.
func TestSetCookieFullFidelityConformance(t *testing.T) {
	expires := time.Date(2027, time.March, 4, 5, 6, 7, 0, time.UTC)
	results := runAcrossFrameworks(t, func(r httpx.Router) {
		r.GET("/cookie/full", func(ctx httpx.Context) error {
			ctx.SetCookie(&http.Cookie{
				Name:     "session",
				Value:    "a b",
				Path:     "/app",
				Expires:  expires,
				MaxAge:   600,
				Secure:   true,
				HttpOnly: true,
				SameSite: http.SameSiteLaxMode,
			})
			return ctx.JSON(http.StatusOK, map[string]any{"ok": true})
		})
	}, func() *http.Request {
		return httptest.NewRequest(http.MethodGet, "http://example.com/cookie/full", nil)
	})
	assertMatchesGin(t, results)
}

// TestFrameworkNotFoundStatusConformance covers D1: an unregistered route must
// report 404 on every adapter (not 500 from an unclassified framework error).
func TestFrameworkNotFoundStatusConformance(t *testing.T) {
	for _, name := range conformanceFrameworks {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, name)
			h.Router.GET("/known", func(ctx httpx.Context) error {
				return ctx.Text(http.StatusOK, "ok")
			})

			got := h.Do(t, httptest.NewRequest(http.MethodGet, "http://example.com/nope", nil))
			if got.Status != http.StatusNotFound {
				t.Fatalf("%s 404 status = %d, want 404; body=%q", name, got.Status, got.Body)
			}
		})
	}
}

// TestErrorAfterCommittedResponseConformance covers D2: a handler that writes
// a response and then returns an error must not have the response corrupted
// by a second error body on any adapter.
func TestErrorAfterCommittedResponseConformance(t *testing.T) {
	for _, name := range conformanceFrameworks {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, name)
			h.Router.GET("/committed/error", func(ctx httpx.Context) error {
				if err := ctx.JSON(http.StatusOK, map[string]any{"ok": true}); err != nil {
					return err
				}
				return errors.New("late failure")
			})

			got := h.Do(t, httptest.NewRequest(http.MethodGet, "http://example.com/committed/error", nil))
			if got.Status != http.StatusOK {
				t.Fatalf("%s status = %d, want 200; body=%q", name, got.Status, got.Body)
			}
			var payload map[string]any
			if err := json.Unmarshal([]byte(got.Body), &payload); err != nil {
				t.Fatalf("%s response corrupted (not a single JSON document): %v; body=%q", name, err, got.Body)
			}
			if payload["ok"] != true {
				t.Fatalf("%s body = %q, want the committed body", name, got.Body)
			}
		})
	}
}

// TestInvalidRedirectCodeConformance covers A8: an invalid redirect code must
// surface as a rendered error on every adapter instead of panicking (gin) or
// being silently rewritten to 302 (hertz).
func TestInvalidRedirectCodeConformance(t *testing.T) {
	for _, name := range conformanceFrameworks {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, name)
			h.Router.GET("/redirect/bad", func(ctx httpx.Context) error {
				return ctx.Redirect(999, "http://example.com/elsewhere")
			})

			got := h.Do(t, httptest.NewRequest(http.MethodGet, "http://example.com/redirect/bad", nil))
			if got.Status != http.StatusInternalServerError {
				t.Fatalf("%s status = %d, want 500; body=%q", name, got.Status, got.Body)
			}
			if got.Headers.Get("Location") != "" {
				t.Fatalf("%s must not set Location for invalid code, got %q", name, got.Headers.Get("Location"))
			}
		})
	}
}

// TestJSONMarshalErrorConformance covers C3: an unmarshalable value must be
// reported as an error (rendered by the error handler) instead of panicking.
func TestJSONMarshalErrorConformance(t *testing.T) {
	for _, name := range conformanceFrameworks {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, name)
			h.Router.GET("/json/badvalue", func(ctx httpx.Context) error {
				return ctx.JSON(http.StatusOK, map[string]any{"ch": make(chan int)})
			})

			got := h.Do(t, httptest.NewRequest(http.MethodGet, "http://example.com/json/badvalue", nil))
			if got.Status != http.StatusInternalServerError {
				t.Fatalf("%s status = %d, want 500; body=%q", name, got.Status, got.Body)
			}
		})
	}
}

// TestStaticPrefixBoundaryConformance covers D9: a URL that merely shares a
// string prefix with the static mount must not serve files from it.
func TestStaticPrefixBoundaryConformance(t *testing.T) {
	tmp := t.TempDir()
	writeStaticFile(t, tmp, "hello.txt", "static-content")

	for _, name := range conformanceFrameworks {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, name)
			h.Router.Static("/assets", tmp)

			ok := h.Do(t, httptest.NewRequest(http.MethodGet, "http://example.com/assets/hello.txt", nil))
			if ok.Status != http.StatusOK || ok.Body != "static-content" {
				t.Fatalf("%s sanity GET failed: status=%d body=%q", name, ok.Status, ok.Body)
			}

			leak := h.Do(t, httptest.NewRequest(http.MethodGet, "http://example.com/assetshello.txt", nil))
			if leak.Status == http.StatusOK && strings.Contains(leak.Body, "static-content") {
				t.Fatalf("%s prefix-adjacent URL leaked static content", name)
			}
		})
	}
}

// TestRootCatchAllMatchesRootConformance pins that a root-level catch-all
// ("/*filepath") also matches the bare "/" on every adapter. Downstream
// mounts (e.g. a std http.ServeMux serving a whole site) rely on this so
// they do not have to register "/" separately — which gin's router would
// reject as a conflicting route.
func TestRootCatchAllMatchesRootConformance(t *testing.T) {
	for _, name := range conformanceFrameworks {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, name)
			h.Router.Handle("GET", "/*filepath", func(ctx httpx.Context) error {
				return ctx.Text(http.StatusOK, "hit:"+ctx.Path())
			})

			for _, reqPath := range []string{"/", "/a/b.txt"} {
				got := h.Do(t, httptest.NewRequest(http.MethodGet, "http://example.com"+reqPath, nil))
				if got.Status != http.StatusOK {
					t.Fatalf("%s GET %s status = %d, want 200; body=%q", name, reqPath, got.Status, got.Body)
				}
				if got.Body != "hit:"+reqPath {
					t.Fatalf("%s GET %s body = %q, want %q", name, reqPath, got.Body, "hit:"+reqPath)
				}
			}
		})
	}
}
