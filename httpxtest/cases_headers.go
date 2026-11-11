package httpxtest

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-sphere/httpx"
)

func init() {
	register("Headers", casesHeaders)
}

// The Headers cases pin how a Content-Type set before a write meets the one
// the write sets, and what ResponseHeaderEditor reports: the headers the
// response will carry, never one it dropped or one nothing set.
func casesHeaders(t *testing.T, r runner) {
	// JSON keeps a Content-Type the handler chose (application/problem+json,
	// say); Text, Bytes and DataFromReader with a type always send their own.
	t.Run("ContentTypePrecedence", func(t *testing.T) {
		for _, tc := range []struct {
			name, want string
			write      func(httpx.Context) error
		}{
			{"JSONKeepsPreset", "application/problem+json", func(ctx httpx.Context) error {
				return ctx.JSON(http.StatusOK, map[string]string{"a": "b"})
			}},
			{"TextReplacesPreset", "text/plain; charset=utf-8", func(ctx httpx.Context) error {
				return ctx.Text(http.StatusOK, "text")
			}},
			{"BytesReplacesPreset", "image/png", func(ctx httpx.Context) error {
				return ctx.Bytes(http.StatusOK, []byte("png"), "image/png")
			}},
			{"DataFromReaderReplacesPreset", "text/csv", func(ctx httpx.Context) error {
				return ctx.DataFromReader(http.StatusOK, "text/csv", strings.NewReader("a,b"), 3)
			}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				got := r.serve(t, func(router httpx.Router) {
					router.GET("/headers/ct", func(ctx httpx.Context) error {
						ctx.SetHeader("Content-Type", "application/problem+json")
						return tc.write(ctx)
					})
				}, httptest.NewRequest(http.MethodGet, "http://example.com/headers/ct", nil))
				if ct := got.Headers.Get("Content-Type"); ct != tc.want {
					t.Fatalf("Content-Type = %q, want %q", ct, tc.want)
				}
			})
		}
	})

	// A Content-Type nothing set is not reported, whatever default the
	// framework keeps internally.
	t.Run("ContentTypeUnsetIsNotReported", func(t *testing.T) {
		var values []string
		var supported bool
		r.serve(t, func(router httpx.Router) {
			router.GET("/headers/unset", func(ctx httpx.Context) error {
				editor, ok := httpx.AsResponseHeaderEditor(ctx)
				supported = ok
				if ok {
					values = editor.ResponseHeaderValues("Content-Type")
				}
				return ctx.NoContent(http.StatusNoContent)
			})
		}, httptest.NewRequest(http.MethodGet, "http://example.com/headers/unset", nil))
		if !supported {
			r.skipf(t, "%s: Context does not implement httpx.ResponseHeaderEditor", r.suite.Name)
		}
		if values != nil {
			t.Fatalf("ResponseHeaderValues(Content-Type) = %q before anything set it, want nil", values)
		}
	})

	// Header writes after the commit are dropped from the response, and
	// ResponseHeaderValues reports the response as sent: neither a late
	// SetHeader nor the Content-Type a second write would have set.
	t.Run("HeaderWritesAfterCommitAreNotReported", func(t *testing.T) {
		var late, contentType []string
		var supported bool
		got := r.serve(t, func(router httpx.Router) {
			router.GET("/headers/late", func(ctx httpx.Context) error {
				if err := ctx.Text(http.StatusOK, "first"); err != nil {
					return err
				}
				ctx.SetHeader("X-Late", "1")
				ctx.SetCookie(&http.Cookie{Name: "late", Value: "1"})
				if err := ctx.JSON(http.StatusOK, "second"); err != nil {
					return err
				}
				editor, ok := httpx.AsResponseHeaderEditor(ctx)
				supported = ok
				if ok {
					late = editor.ResponseHeaderValues("X-Late")
					contentType = editor.ResponseHeaderValues("Content-Type")
				}
				return nil
			})
		}, httptest.NewRequest(http.MethodGet, "http://example.com/headers/late", nil))
		if got.Headers.Get("X-Late") != "" || got.Headers.Get("Set-Cookie") != "" {
			t.Fatalf("a header set after the commit was sent: %v", got.Headers)
		}
		if !supported {
			r.skipf(t, "%s: Context does not implement httpx.ResponseHeaderEditor", r.suite.Name)
		}
		if late != nil {
			t.Errorf("ResponseHeaderValues(X-Late) = %q after the commit, want nil", late)
		}
		if len(contentType) != 1 || contentType[0] != "text/plain; charset=utf-8" {
			t.Errorf("ResponseHeaderValues(Content-Type) = %q, want the committed %q", contentType, "text/plain; charset=utf-8")
		}
	})
}
