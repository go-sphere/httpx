package httpxtest

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-sphere/httpx"
)

func init() {
	register("Regression", casesRegression)
}

// Cases that exist because an adapter once got them wrong: each one pins a
// behavior that differed between frameworks and had to be normalized.
func casesRegression(t *testing.T, r runner) {
	// A generated route like /files/*filepath must register and resolve
	// everywhere, and the value must not carry a leading slash.
	t.Run("NamedWildcardValue", func(t *testing.T) {
		got := r.serve(t, func(router httpx.Router) {
			router.Handle("GET", "/files/*filepath", func(ctx httpx.Context) error {
				return ctx.JSON(http.StatusOK, map[string]any{
					"param":  ctx.Param("filepath"),
					"params": ctx.Params(),
				})
			})
		}, httptest.NewRequest(http.MethodGet, "http://example.com/files/a/b.txt", nil))

		if got.Status != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%q", got.Status, got.Body)
		}
		var payload struct {
			Param  string            `json:"param"`
			Params map[string]string `json:"params"`
		}
		if err := json.Unmarshal([]byte(got.Body), &payload); err != nil {
			t.Fatalf("parse body: %v; body=%q", err, got.Body)
		}
		if payload.Param != "a/b.txt" {
			t.Fatalf("Param(filepath) = %q, want %q", payload.Param, "a/b.txt")
		}
		if payload.Params["filepath"] != "a/b.txt" {
			t.Fatalf("Params()[filepath] = %q, want %q (params=%v)", payload.Params["filepath"], "a/b.txt", payload.Params)
		}
	})

	// A root-level catch-all must also match the bare "/", which downstream
	// mounts (a std http.ServeMux serving a whole site) rely on.
	t.Run("RootCatchAllMatchesRoot", func(t *testing.T) {
		for _, reqPath := range []string{"/", "/a/b.txt"} {
			got := r.serve(t, func(router httpx.Router) {
				router.Handle("GET", "/*filepath", func(ctx httpx.Context) error {
					return ctx.Text(http.StatusOK, "hit:"+ctx.Path())
				})
			}, httptest.NewRequest(http.MethodGet, "http://example.com"+reqPath, nil))

			if got.Status != http.StatusOK {
				t.Fatalf("GET %s status = %d, want 200; body=%q", reqPath, got.Status, got.Body)
			}
			if got.Body != "hit:"+reqPath {
				t.Fatalf("GET %s body = %q, want %q", reqPath, got.Body, "hit:"+reqPath)
			}
		}
	})

	// Headers() is the request's headers; Host travels separately and must not
	// appear among them.
	t.Run("HeadersExcludeHost", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "http://example.com/headers/nohost", nil)
		req.Header.Set("X-Trace-In", "t1")
		r.assertGolden(t, func(router httpx.Router) {
			router.GET("/headers/nohost", func(ctx httpx.Context) error {
				_, hasHost := ctx.Headers()["Host"]
				return ctx.JSON(http.StatusOK, map[string]any{
					"hasHost": hasHost,
					"trace":   ctx.Header("X-Trace-In"),
				})
			})
		}, req)
	})

	// A cookie with Expires, a space in the value and every flag set must
	// serialize identically on every adapter.
	t.Run("SetCookieFullFidelity", func(t *testing.T) {
		expires := time.Date(2027, time.March, 4, 5, 6, 7, 0, time.UTC)
		r.assertGolden(t, func(router httpx.Router) {
			router.GET("/cookie/full", func(ctx httpx.Context) error {
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
		}, httptest.NewRequest(http.MethodGet, "http://example.com/cookie/full", nil))
	})

	// An unregistered route is 404, not a 500 from an unclassified framework
	// error.
	t.Run("NotFoundStatus", func(t *testing.T) {
		got := r.serve(t, func(router httpx.Router) {
			router.GET("/known", func(ctx httpx.Context) error {
				return ctx.Text(http.StatusOK, "ok")
			})
		}, httptest.NewRequest(http.MethodGet, "http://example.com/nope", nil))

		if got.Status != http.StatusNotFound {
			t.Fatalf("status = %d, want 404; body=%q", got.Status, got.Body)
		}
	})

	// A handler that writes a response and then fails must not have the
	// response corrupted by a second error body.
	t.Run("ErrorAfterCommittedResponse", func(t *testing.T) {
		got := r.serve(t, func(router httpx.Router) {
			router.GET("/committed/error", func(ctx httpx.Context) error {
				if err := ctx.JSON(http.StatusOK, map[string]any{"ok": true}); err != nil {
					return err
				}
				return errors.New("late failure")
			})
		}, httptest.NewRequest(http.MethodGet, "http://example.com/committed/error", nil))

		if got.Status != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%q", got.Status, got.Body)
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(got.Body), &payload); err != nil {
			t.Fatalf("response is not a single JSON document: %v; body=%q", err, got.Body)
		}
		if payload["ok"] != true {
			t.Fatalf("body = %q, want the committed body", got.Body)
		}
	})

	// The same, one layer out: a middleware that fails after the handler
	// committed must not overwrite the response either.
	t.Run("MiddlewareErrorAfterCommittedResponse", func(t *testing.T) {
		got := r.serveWith(t, Options{ErrorHandler: teapotErrorHandler}, func(router httpx.Router) {
			router.Use(func(ctx httpx.Context) error {
				if err := ctx.Next(); err != nil {
					return err
				}
				return errors.New("late middleware error")
			})
			router.GET("/committed/middleware", func(ctx httpx.Context) error {
				return ctx.Text(http.StatusOK, "ok")
			})
		}, httptest.NewRequest(http.MethodGet, "http://example.com/committed/middleware", nil))

		if got.Status != http.StatusOK || got.Body != "ok" {
			t.Fatalf("response = %d %q, want 200 %q", got.Status, got.Body, "ok")
		}
	})

	// Not rendering an error over a committed response must not mean losing
	// it: an outer layer still has to see the failure, or a handler that fails
	// after writing its response becomes invisible to logging and metrics.
	// Every adapter keeps it on its own error path (gin/hertz append to the
	// native error list, echo returns it to echo, fiber parks it on the
	// context because returning it would let fiber render over the body).
	t.Run("OuterLayerSeesErrorAfterCommittedResponse", func(t *testing.T) {
		var seen error
		got := r.serve(t, func(router httpx.Router) {
			router.Use(func(ctx httpx.Context) error {
				seen = ctx.Next()
				// Swallow it: the point of the case is that it arrived, and
				// returning it would only re-enter the same path.
				return nil
			})
			router.GET("/committed/observed", func(ctx httpx.Context) error {
				if err := ctx.Text(http.StatusOK, "ok"); err != nil {
					return err
				}
				return errors.New("late failure")
			})
		}, httptest.NewRequest(http.MethodGet, "http://example.com/committed/observed", nil))

		if got.Status != http.StatusOK || got.Body != "ok" {
			t.Fatalf("response = %d %q, want 200 %q", got.Status, got.Body, "ok")
		}
		if seen == nil {
			t.Fatal("the middleware's Next returned nil: an error raised after the response was committed was dropped")
		}
		if !strings.Contains(seen.Error(), "late failure") {
			t.Fatalf("Next returned %v, want it to carry \"late failure\"", seen)
		}
	})

	// An invalid redirect code is a rendered error, not a panic (gin) and not
	// a silent rewrite to 302 (hertz).
	t.Run("InvalidRedirectCode", func(t *testing.T) {
		got := r.serve(t, func(router httpx.Router) {
			router.GET("/redirect/bad", func(ctx httpx.Context) error {
				return ctx.Redirect(999, "http://example.com/elsewhere")
			})
		}, httptest.NewRequest(http.MethodGet, "http://example.com/redirect/bad", nil))

		if got.Status != http.StatusInternalServerError {
			t.Fatalf("status = %d, want 500; body=%q", got.Status, got.Body)
		}
		if loc := got.Headers.Get("Location"); loc != "" {
			t.Fatalf("Location = %q, want it unset for an invalid code", loc)
		}
	})

	// An unmarshalable value is an error through the error handler, not a
	// panic.
	t.Run("JSONMarshalError", func(t *testing.T) {
		got := r.serve(t, func(router httpx.Router) {
			router.GET("/json/badvalue", func(ctx httpx.Context) error {
				return ctx.JSON(http.StatusOK, map[string]any{"ch": make(chan int)})
			})
		}, httptest.NewRequest(http.MethodGet, "http://example.com/json/badvalue", nil))

		if got.Status != http.StatusInternalServerError {
			t.Fatalf("status = %d, want 500; body=%q", got.Status, got.Body)
		}
	})

	// A URL that merely shares a string prefix with the static mount must not
	// serve files from it.
	t.Run("StaticPrefixBoundary", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("static-content"), 0o600); err != nil {
			t.Fatalf("write static file: %v", err)
		}
		register := func(router httpx.Router) { router.Static("/assets", dir) }

		ok := r.serve(t, register, httptest.NewRequest(http.MethodGet, "http://example.com/assets/hello.txt", nil))
		if ok.Status != http.StatusOK || ok.Body != "static-content" {
			t.Fatalf("sanity GET failed: status=%d body=%q", ok.Status, ok.Body)
		}
		leak := r.serve(t, register, httptest.NewRequest(http.MethodGet, "http://example.com/assetshello.txt", nil))
		if leak.Status == http.StatusOK && strings.Contains(leak.Body, "static-content") {
			t.Fatalf("prefix-adjacent URL leaked static content: %d %q", leak.Status, leak.Body)
		}
	})

	// Binder targets that are not a plain struct: a multi-level pointer must
	// decode and validate, and a slice with an invalid element must be
	// rejected.
	t.Run("PointerAndSliceValidation", func(t *testing.T) {
		type item struct {
			Name string `json:"name" binding:"required"`
		}
		register := func(router httpx.Router) {
			router.POST("/validate/ptr", func(ctx httpx.Context) error {
				var dst *item
				if err := ctx.BindJSON(&dst); err != nil {
					return err
				}
				return ctx.JSON(http.StatusOK, map[string]any{"name": dst.Name})
			})
			router.POST("/validate/slice", func(ctx httpx.Context) error {
				var dst []*item
				if err := ctx.BindJSON(&dst); err != nil {
					return err
				}
				// Reached only when every element is non-null, which is the
				// point of rejecting a null element rather than passing it on.
				names := make([]string, 0, len(dst))
				for _, it := range dst {
					names = append(names, it.Name)
				}
				return ctx.JSON(http.StatusOK, map[string]any{"count": len(dst), "names": names})
			})
		}
		jsonReq := func(path, body string) *http.Request {
			req := httptest.NewRequest(http.MethodPost, "http://example.com"+path, strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			return req
		}

		for _, tc := range []struct {
			name, path, body string
			want             int
		}{
			// A null element cannot satisfy the element rules and would
			// nil-dereference in the handler; gin's validator panics on it,
			// so every adapter reports it as a bind failure instead.
			{"slice with a null element", "/validate/slice", `[{"name":"ok"},null]`, http.StatusBadRequest},
			{"invalid pointer target", "/validate/ptr", `{}`, http.StatusBadRequest},
			{"valid pointer target", "/validate/ptr", `{"name":"ok"}`, http.StatusOK},
			{"valid slice target", "/validate/slice", `[{"name":"ok"},{"name":"ok"}]`, http.StatusOK},
			{"slice with invalid element", "/validate/slice", `[{"name":"ok"},{}]`, http.StatusBadRequest},
		} {
			got := r.serve(t, register, jsonReq(tc.path, tc.body))
			if got.Status != tc.want {
				t.Fatalf("%s: status = %d, want %d; body=%q", tc.name, got.Status, tc.want, got.Body)
			}
		}
	})
}

// teapotErrorHandler is a custom handler that renders without aborting, which
// is how the cases check that the adapter — not the handler — is what stops
// the chain.
func teapotErrorHandler(ctx httpx.Context, err error) {
	_ = ctx.JSON(http.StatusTeapot, map[string]any{"error": err.Error()})
}

func init() {
	register("CommittedResponse", casesCommittedResponse)
}

// A response the handler already decided must never be replaced by an error
// body. The cases that wrote a body were already covered; these are the ones
// that decide a response *without* writing bytes, where an adapter that
// detects commitment by "has a body" gets it wrong.
func casesCommittedResponse(t *testing.T, r runner) {
	t.Run("NoContentThenError", func(t *testing.T) {
		got := r.serve(t, func(router httpx.Router) {
			router.GET("/committed/nocontent", func(ctx httpx.Context) error {
				if err := ctx.NoContent(http.StatusNoContent); err != nil {
					return err
				}
				return errors.New("late failure")
			})
		}, httptest.NewRequest(http.MethodGet, "http://example.com/committed/nocontent", nil))

		if got.Status != http.StatusNoContent {
			t.Fatalf("status = %d, want %d: the error was rendered over a committed bodyless response; body=%q",
				got.Status, http.StatusNoContent, got.Body)
		}
		if got.Body != "" {
			t.Fatalf("body = %q, want it empty", got.Body)
		}
	})

	// The other direction: a bare Status(code) records a code without
	// producing a response, so a later error must still be rendered. Treating
	// it as committed would turn a failure into a silent 2xx with no body.
	t.Run("StatusAloneDoesNotSwallowError", func(t *testing.T) {
		got := r.serve(t, func(router httpx.Router) {
			router.GET("/committed/status", func(ctx httpx.Context) error {
				ctx.Status(http.StatusAccepted)
				return errors.New("late failure")
			})
		}, httptest.NewRequest(http.MethodGet, "http://example.com/committed/status", nil))

		if got.Status != http.StatusInternalServerError {
			t.Fatalf("status = %d, want %d: a bare Status must not hide the error; body=%q",
				got.Status, http.StatusInternalServerError, got.Body)
		}
	})

	t.Run("RedirectThenError", func(t *testing.T) {
		got := r.serve(t, func(router httpx.Router) {
			router.GET("/committed/redirect", func(ctx httpx.Context) error {
				if err := ctx.Redirect(http.StatusFound, "/to"); err != nil {
					return err
				}
				return errors.New("late failure")
			})
		}, httptest.NewRequest(http.MethodGet, "http://example.com/committed/redirect", nil))

		if got.Status != http.StatusFound {
			t.Fatalf("status = %d, want %d; body=%q", got.Status, http.StatusFound, got.Body)
		}
		if loc := got.Headers.Get("Location"); loc != "/to" {
			t.Fatalf("Location = %q, want %q", loc, "/to")
		}
	})

	// ServerSentEvents commits a 200 before the callback runs, so an error
	// from the callback cannot change the status any more. The error is still
	// recorded on the framework's error path where the adapter has one; what
	// must not happen is an error body replacing the stream.
	t.Run("StreamErrorBeforeFirstEvent", func(t *testing.T) {
		got := r.serve(t, func(router httpx.Router) {
			router.GET("/committed/stream", func(ctx httpx.Context) error {
				return httpx.ServerSentEvents(ctx, func(w *httpx.SSEWriter) error {
					return errors.New("failed before the first event")
				})
			})
		}, httptest.NewRequest(http.MethodGet, "http://example.com/committed/stream", nil))

		if got.Status != http.StatusOK {
			t.Fatalf("status = %d, want 200: the stream had already committed it; body=%q", got.Status, got.Body)
		}
		if strings.Contains(got.Body, "failed before the first event") {
			t.Fatalf("the error body replaced the stream: %q", got.Body)
		}
	})

	// A stream decides the response even on adapters whose callback runs after
	// the handler returns (fiber), so a failure raised *after* Stream must not
	// be rendered over it. Detecting this by "has a body" fails: the body is
	// still empty when the handler returns.
	t.Run("StreamThenError", func(t *testing.T) {
		got := r.serveWith(t, Options{ErrorHandler: teapotErrorHandler}, func(router httpx.Router) {
			router.GET("/committed/stream-then-error", func(ctx httpx.Context) error {
				streamer, ok := httpx.AsStreamer(ctx)
				if !ok {
					return errors.New("adapter does not implement httpx.Streamer")
				}
				if err := streamer.Stream(http.StatusOK, "text/plain", func(w io.Writer) error {
					_, err := w.Write([]byte("streamed-body"))
					return err
				}); err != nil {
					return err
				}
				return errors.New("failure after the stream was handed over")
			})
		}, httptest.NewRequest(http.MethodGet, "http://example.com/committed/stream-then-error", nil))

		if got.Status != http.StatusOK {
			t.Fatalf("status = %d, want 200: the error was rendered over a stream; body=%q", got.Status, got.Body)
		}
		if got.Body != "streamed-body" {
			t.Fatalf("body = %q, want %q: the stream was replaced", got.Body, "streamed-body")
		}
	})

	// The same failure one layer out: a middleware that fails after the
	// handler handed over the stream.
	t.Run("MiddlewareErrorAfterStream", func(t *testing.T) {
		got := r.serveWith(t, Options{ErrorHandler: teapotErrorHandler}, func(router httpx.Router) {
			router.Use(func(ctx httpx.Context) error {
				if err := ctx.Next(); err != nil {
					return err
				}
				return errors.New("late middleware failure")
			})
			router.GET("/committed/stream-then-middleware", func(ctx httpx.Context) error {
				streamer, ok := httpx.AsStreamer(ctx)
				if !ok {
					return errors.New("adapter does not implement httpx.Streamer")
				}
				return streamer.Stream(http.StatusOK, "text/plain", func(w io.Writer) error {
					_, err := w.Write([]byte("streamed-body"))
					return err
				})
			})
		}, httptest.NewRequest(http.MethodGet, "http://example.com/committed/stream-then-middleware", nil))

		if got.Status != http.StatusOK || got.Body != "streamed-body" {
			t.Fatalf("response = %d %q, want 200 %q", got.Status, got.Body, "streamed-body")
		}
	})
}
