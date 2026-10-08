package httpxtest

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-sphere/httpx"
)

func init() {
	register("BodyLimit", casesBodyLimit)
}

// statusTextErrorHandler renders an error as "err:<status>", so a case can
// tell the configured handler answered and which status it classified.
func statusTextErrorHandler(ctx httpx.Context, err error) {
	status, _ := httpx.RenderError(err)
	_ = ctx.Text(status, fmt.Sprintf("err:%d", status))
}

// The request body limit (Options.MaxBodySize, the adapter's WithMaxBodySize):
// an oversized body is a 413 rendered by the configured error handler, whether
// the adapter refuses it up front or the read that passes the limit fails.
func casesBodyLimit(t *testing.T, r runner) {
	const limit = 16
	opts := Options{ErrorHandler: statusTextErrorHandler, MaxBodySize: limit}
	oversized := `{"name":"` + strings.Repeat("x", 4*limit) + `"}`

	t.Run("DeclaredLengthOverLimitIs413", func(t *testing.T) {
		var reached []string
		got := r.serveWith(t, opts, func(router httpx.Router) {
			router.Use(func(next httpx.Handler) httpx.Handler {
				return func(ctx httpx.Context) error {
					reached = append(reached, "middleware")
					return next(ctx)
				}
			})
			router.POST("/limit/declared", func(ctx httpx.Context) error {
				reached = append(reached, "handler")
				return ctx.NoContent(http.StatusNoContent)
			})
		}, httptest.NewRequest(http.MethodPost, "http://example.com/limit/declared", strings.NewReader(oversized)))
		if got.Status != http.StatusRequestEntityTooLarge || got.Body != "err:413" {
			t.Fatalf("status=%d body=%q, want 413 %q", got.Status, got.Body, "err:413")
		}
		if len(reached) != 0 {
			t.Fatalf("reached %v; a body declared over the limit must be refused before the route's middleware", reached)
		}
	})

	t.Run("BodyAtLimitIsServed", func(t *testing.T) {
		body := strings.Repeat("y", limit)
		got := r.serveWith(t, opts, func(router httpx.Router) {
			router.POST("/limit/exact", func(ctx httpx.Context) error {
				raw, err := ctx.BodyRaw()
				if err != nil {
					return err
				}
				return ctx.Text(http.StatusOK, string(raw))
			})
		}, httptest.NewRequest(http.MethodPost, "http://example.com/limit/exact", strings.NewReader(body)))
		if got.Status != http.StatusOK || got.Body != body {
			t.Fatalf("status=%d body=%q, want 200 %q", got.Status, got.Body, body)
		}
	})

	// A body whose length is not declared cannot be refused up front by an
	// adapter that streams it; the read that passes the limit fails instead,
	// and both a bind error and a raw read error returned as is are 413.
	t.Run("UnknownLengthOverLimitIs413", func(t *testing.T) {
		if !r.suite.Caps.InProcessUnknownLengthBody {
			t.Skipf("%s: Caps.InProcessUnknownLengthBody is not declared; the in-process requester cannot express an unknown body length", r.suite.Name)
		}
		for _, tc := range []struct {
			name    string
			handler httpx.Handler
		}{
			{"BindJSON", func(ctx httpx.Context) error {
				var in struct {
					Name string `json:"name"`
				}
				if err := ctx.BindJSON(&in); err != nil {
					return err
				}
				return ctx.Text(http.StatusOK, in.Name)
			}},
			{"BodyRaw", func(ctx httpx.Context) error {
				raw, err := ctx.BodyRaw()
				if err != nil {
					return err
				}
				return ctx.Text(http.StatusOK, string(raw))
			}},
			{"BodyReader", func(ctx httpx.Context) error {
				raw, err := io.ReadAll(ctx.BodyReader())
				if err != nil {
					return err
				}
				return ctx.Text(http.StatusOK, string(raw))
			}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				req := httptest.NewRequest(http.MethodPost, "http://example.com/limit/unknown", io.NopCloser(strings.NewReader(oversized)))
				req.ContentLength = -1
				req.Header.Set("Content-Type", "application/json")
				got := r.serveWith(t, opts, func(router httpx.Router) {
					router.POST("/limit/unknown", tc.handler)
				}, req)
				if got.Status != http.StatusRequestEntityTooLarge || got.Body != "err:413" {
					t.Fatalf("status=%d body=%q, want 413 %q", got.Status, got.Body, "err:413")
				}
			})
		}
	})

	t.Run("NoLimitByDefault", func(t *testing.T) {
		body := strings.Repeat("z", 64<<10)
		got := r.serveWith(t, Options{ErrorHandler: statusTextErrorHandler}, func(router httpx.Router) {
			router.POST("/limit/none", func(ctx httpx.Context) error {
				raw, err := ctx.BodyRaw()
				if err != nil {
					return err
				}
				return ctx.Text(http.StatusOK, fmt.Sprint(len(raw)))
			})
		}, httptest.NewRequest(http.MethodPost, "http://example.com/limit/none", strings.NewReader(body)))
		if want := fmt.Sprint(len(body)); got.Status != http.StatusOK || got.Body != want {
			t.Fatalf("status=%d body=%q, want 200 %q", got.Status, got.Body, want)
		}
	})
}
