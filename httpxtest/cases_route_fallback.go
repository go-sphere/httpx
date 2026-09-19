package httpxtest

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-sphere/httpx"
)

func init() {
	register("RouteFallback", casesRouteFallback)
}

// What every adapter answers for a request no route handled. The frameworks
// disagree natively — gin and hertz report a plain-text 404 for a wrong method
// and never reach the configured error handler — so the adapters normalize it:
// an unmatched path is 404, a path that exists under another method is 405, and
// both are rendered by the engine's httpx.ErrorHandler with a real
// adapter-backed Context, exactly like a route that returns an error.
func casesRouteFallback(t *testing.T, r runner) {
	// One route makes the two cases distinguishable: /nope matches nothing,
	// /items/1 matches under another method.
	register := func(router httpx.Router) {
		router.POST("/items/:id", func(ctx httpx.Context) error {
			return ctx.Text(http.StatusOK, "created")
		})
	}

	t.Run("NotFound", func(t *testing.T) {
		r.assertGolden(t, register, httptest.NewRequest(http.MethodGet, "http://example.com/nope", nil))
	})

	t.Run("MethodNotAllowed", func(t *testing.T) {
		r.assertGolden(t, register, httptest.NewRequest(http.MethodGet, "http://example.com/items/1", nil))
	})

	// The golden contract drops the charset (see contractOf), so the exact
	// Content-Type is asserted here: echo labels JSON "application/json" and the
	// other four add "; charset=utf-8", which a client trusting the label rather
	// than sniffing can tell apart.
	t.Run("ContentTypeIsJSONUTF8", func(t *testing.T) {
		for _, tc := range []struct {
			name, target string
			status       int
		}{
			{"unmatched path", "/nope", http.StatusNotFound},
			{"wrong method", "/items/1", http.StatusMethodNotAllowed},
		} {
			got := r.serve(t, register, httptest.NewRequest(http.MethodGet, "http://example.com"+tc.target, nil))
			if got.Status != tc.status {
				t.Fatalf("%s: status = %d, want %d; body=%q", tc.name, got.Status, tc.status, got.Body)
			}
			if ct := got.Headers.Get("Content-Type"); ct != "application/json; charset=utf-8" {
				t.Fatalf("%s: Content-Type = %q, want %q", tc.name, ct, "application/json; charset=utf-8")
			}
		}
	})

	// A custom httpx.ErrorHandler owns the unmatched-path answer too: gin and
	// hertz would otherwise write their own plain-text body without consulting
	// it, which is how an error envelope applies to every response except the
	// ones the application did not route.
	t.Run("CustomErrorHandlerRendersFallback", func(t *testing.T) {
		for _, tc := range []struct {
			name, target string
			wantStatus   int
		}{
			{"unmatched path", "/nope", http.StatusNotFound},
			{"wrong method", "/items/1", http.StatusMethodNotAllowed},
		} {
			got := r.serveWith(t, Options{ErrorHandler: func(ctx httpx.Context, err error) {
				_, status, message := httpx.ClassifyError(err)
				_ = ctx.Text(int(status), "custom:"+message)
			}}, register, httptest.NewRequest(http.MethodGet, "http://example.com"+tc.target, nil))

			if got.Status != tc.wantStatus {
				t.Fatalf("%s: status = %d, want %d; body=%q", tc.name, got.Status, tc.wantStatus, got.Body)
			}
			if want := "custom:" + http.StatusText(tc.wantStatus); got.Body != want {
				t.Fatalf("%s: body = %q, want %q: the fallback bypassed the configured error handler",
					tc.name, got.Body, want)
			}
		}
	})

	// An error handler may render nothing — logging and leaving the body to a
	// layer above is legitimate — but the adapter still owes a status, and the
	// error's own status is the floor: an unmatched path must not answer the 200
	// a response starts at, which caches and monitoring believe. The body is not
	// asserted because the adapters disagree on purpose: gin and hertz write
	// their framework's plain-text default, the other three answer empty.
	t.Run("SilentErrorHandlerCommitsTheErrorStatus", func(t *testing.T) {
		for _, tc := range []struct {
			name, target string
			wantStatus   int
		}{
			{"unmatched path", "/nope", http.StatusNotFound},
			{"wrong method", "/items/1", http.StatusMethodNotAllowed},
		} {
			got := r.serveWith(t, Options{ErrorHandler: func(ctx httpx.Context, err error) {
				// Reads the error and renders nothing, like a logging-only
				// handler.
				_, _, _ = httpx.ClassifyError(err)
			}}, register, httptest.NewRequest(http.MethodGet, "http://example.com"+tc.target, nil))

			if got.Status != tc.wantStatus {
				t.Fatalf("%s: status = %d, want %d: an error handler that rendered nothing must not leave the response at 200; body=%q",
					tc.name, got.Status, tc.wantStatus, got.Body)
			}
		}
	})

	// The same rule one layer in: a handler that only sets a status still owns
	// the response, and a status it chose is never replaced by the error's.
	t.Run("StatusOnlyErrorHandlerKeepsItsStatus", func(t *testing.T) {
		got := r.serveWith(t, Options{ErrorHandler: func(ctx httpx.Context, err error) {
			ctx.SetHeader("X-Handled", "1")
			ctx.Status(http.StatusTeapot)
		}}, register, httptest.NewRequest(http.MethodGet, "http://example.com/nope", nil))

		if got.Status != http.StatusTeapot {
			t.Fatalf("status = %d, want 418: a status the error handler set must win over the error's; body=%q",
				got.Status, got.Body)
		}
		if got.Headers.Get("X-Handled") != "1" {
			t.Fatalf("X-Handled = %q, want %q", got.Headers.Get("X-Handled"), "1")
		}
	})

	// Engine-wide middleware is what an access log or a recovery layer is
	// registered on, so it must keep running for a request no route matched, or
	// those layers go blind exactly where a misrouted request needs them.
	t.Run("EngineMiddlewareRunsOnNotFound", func(t *testing.T) {
		engine := r.suite.NewEngine(t, Options{})
		var order []string
		engine.Use(func(next httpx.Handler) httpx.Handler {
			return func(ctx httpx.Context) error {
				order = append(order, "engine-before")
				err := next(ctx)
				order = append(order, "engine-after")
				return err
			}
		})
		register(engine.Group(""))

		requester, ok := httpx.AsTestRequester(engine)
		if !ok {
			t.Fatalf("%s: engine does not support httpx.TestRequester", r.suite.Name)
		}
		resp, err := requester.Do(httptest.NewRequest(http.MethodGet, "http://example.com/nope", nil))
		if err != nil {
			t.Fatalf("%s: serve: %v", r.suite.Name, err)
		}
		_ = resp.Body.Close()

		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", resp.StatusCode)
		}
		if got := strings.Join(order, ","); got != "engine-before,engine-after" {
			t.Fatalf("engine middleware order = %q, want %q: it did not wrap the unmatched path",
				got, "engine-before,engine-after")
		}
	})
}
