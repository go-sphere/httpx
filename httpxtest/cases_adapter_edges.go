package httpxtest

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/go-sphere/httpx"
)

func init() {
	register("AdapterEdges", casesAdapterEdges)
}

type trackingCloser struct {
	r      io.Reader
	read   int
	closed bool
}

func (tc *trackingCloser) Read(p []byte) (int, error) {
	n, err := tc.r.Read(p)
	tc.read += n
	return n, err
}

func (tc *trackingCloser) Close() error {
	tc.closed = true
	return nil
}

type flushCountWriter struct {
	http.ResponseWriter
	flushes int
}

func (f *flushCountWriter) Flush() {
	f.flushes++
	if fl, ok := f.ResponseWriter.(http.Flusher); ok {
		fl.Flush()
	}
}

type unwrapOnlyWriter struct {
	w http.ResponseWriter
}

func (u *unwrapOnlyWriter) Header() http.Header         { return u.w.Header() }
func (u *unwrapOnlyWriter) Write(p []byte) (int, error) { return u.w.Write(p) }
func (u *unwrapOnlyWriter) WriteHeader(code int)        { u.w.WriteHeader(code) }
func (u *unwrapOnlyWriter) Unwrap() http.ResponseWriter { return u.w }

type hintCapturingWriter struct {
	http.ResponseWriter
	hints []int
}

func (h *hintCapturingWriter) WriteHeader(code int) {
	if code >= 100 && code < 200 && code != http.StatusSwitchingProtocols {
		h.hints = append(h.hints, code)
		return
	}
	h.ResponseWriter.WriteHeader(code)
}

func casesAdapterEdges(t *testing.T, r runner) {
	t.Run("SetCookieReplaceAndMultiCookieValues", func(t *testing.T) {
		var beforeReplace, afterReplace []string
		got := r.serve(t, func(router httpx.Router) {
			router.GET("/cookies", func(ctx httpx.Context) error {
				ctx.SetCookie(&http.Cookie{Name: "a", Value: "1", Path: "/"})
				ctx.SetCookie(&http.Cookie{Name: "b", Value: "2", Path: "/"})
				editor, ok := httpx.AsResponseHeaderEditor(ctx)
				if ok {
					beforeReplace = editor.ResponseHeaderValues("Set-Cookie")
				}
				ctx.SetHeader("Set-Cookie", "final=1; Path=/")
				if ok {
					afterReplace = editor.ResponseHeaderValues("Set-Cookie")
				}
				return ctx.NoContent(http.StatusNoContent)
			})
		}, httptest.NewRequest(http.MethodGet, "http://example.com/cookies", nil))

		if len(beforeReplace) != 2 {
			t.Fatalf("ResponseHeaderValues before SetHeader = %q, want 2 cookies", beforeReplace)
		}
		if !slices.Equal(afterReplace, []string{"final=1; Path=/"}) {
			t.Fatalf("ResponseHeaderValues after SetHeader = %q, want [final=1; Path=/]", afterReplace)
		}
		if vals := got.Headers.Values("Set-Cookie"); len(vals) != 1 || !strings.HasPrefix(vals[0], "final=1") {
			t.Fatalf("final Set-Cookie = %q, want single final=1 cookie", vals)
		}
	})

	t.Run("DataFromReader204UnreadNoContentLength", func(t *testing.T) {
		rc := &trackingCloser{r: strings.NewReader("should-not-read")}
		got := r.serve(t, func(router httpx.Router) {
			router.GET("/204-reader", func(ctx httpx.Context) error {
				return ctx.DataFromReader(http.StatusNoContent, "text/plain", rc, 15)
			})
		}, httptest.NewRequest(http.MethodGet, "http://example.com/204-reader", nil))

		if got.Status != http.StatusNoContent || got.Body != "" {
			t.Fatalf("status=%d body=%q, want 204 empty", got.Status, got.Body)
		}
		if cl := got.Headers.Get("Content-Length"); cl != "" && cl != "0" {
			t.Fatalf("Content-Length = %q on 204, want absent/0", cl)
		}
		if rc.read != 0 || !rc.closed {
			t.Fatalf("reader read=%d closed=%v, want read=0 closed=true", rc.read, rc.closed)
		}
	})

	t.Run("PostCommitBodylessAndJSON204EncodeError", func(t *testing.T) {
		got204 := r.serve(t, func(router httpx.Router) {
			router.GET("/204-200", func(ctx httpx.Context) error {
				if err := ctx.NoContent(http.StatusNoContent); err != nil {
					return err
				}
				return ctx.Text(http.StatusOK, "should-drop")
			})
		}, httptest.NewRequest(http.MethodGet, "http://example.com/204-200", nil))
		if got204.Status != http.StatusNoContent || got204.Body != "" {
			t.Fatalf("204->200 status=%d body=%q, want 204 empty", got204.Status, got204.Body)
		}

		got200 := r.serve(t, func(router httpx.Router) {
			router.GET("/200-204", func(ctx httpx.Context) error {
				if err := ctx.Text(http.StatusOK, "first-"); err != nil {
					return err
				}
				return ctx.Text(http.StatusNoContent, "second")
			})
		}, httptest.NewRequest(http.MethodGet, "http://example.com/200-204", nil))
		if got200.Status != http.StatusOK || got200.Body != "first-second" {
			t.Fatalf("200->204 status=%d body=%q, want 200 %q", got200.Status, got200.Body, "first-second")
		}

		var jsonErr error
		var committedAfterErr bool
		gotBad := r.serveWith(t, Options{ErrorHandler: statusTextErrorHandler}, func(router httpx.Router) {
			router.GET("/json-204-bad", func(ctx httpx.Context) error {
				jsonErr = ctx.JSON(http.StatusNoContent, make(chan int))
				committedAfterErr = ctx.Committed()
				return jsonErr
			})
		}, httptest.NewRequest(http.MethodGet, "http://example.com/json-204-bad", nil))
		if jsonErr == nil {
			t.Fatal("JSON(204, chan) = nil, want encode error")
		}
		if committedAfterErr {
			t.Fatal("JSON(204, chan) must not commit on encode failure")
		}
		if gotBad.Status != http.StatusInternalServerError {
			t.Fatalf("status = %d, want 500 after JSON(204, chan) failure", gotBad.Status)
		}
	})

	t.Run("MethodNotAllowedCleanParamsAndSilentErrorHandler", func(t *testing.T) {
		var observedParams map[string]string
		var observedFullPath string
		got := r.serveEngine(t, Options{ErrorHandler: func(ctx httpx.Context, err error) {
			_, _, _ = httpx.ClassifyError(err)
		}}, func(engine httpx.Engine) {
			engine.Use(func(next httpx.Handler) httpx.Handler {
				return func(ctx httpx.Context) error {
					observedParams = ctx.Params()
					observedFullPath = ctx.FullPath()
					if err := next(ctx); err != nil {
						return httpx.NewForbiddenError("denied-in-mw")
					}
					return nil
				}
			})
			engine.Group("").POST("/items/:id", func(ctx httpx.Context) error {
				return ctx.Text(http.StatusOK, "ok")
			})
		}, httptest.NewRequest(http.MethodGet, "http://example.com/items/42", nil))

		if len(observedParams) != 0 || observedFullPath != "" {
			t.Fatalf("405 observed params=%v fullPath=%q, want empty", observedParams, observedFullPath)
		}
		if got.Status != http.StatusForbidden {
			t.Fatalf("status = %d, want 403 from middleware error with silent ErrorHandler", got.Status)
		}
	})

	t.Run("AdaptStdMiddlewareEdges", func(t *testing.T) {
		if r.suite.StdMiddleware == nil {
			r.skipf(t, "%s: no StdMiddleware hook declared", r.suite.Name)
		}

		t.Run("FlushAndUnwrap", func(t *testing.T) {
			if !r.suite.Caps.Flusher {
				r.skipf(t, "%s: Caps.Flusher is not declared", r.suite.Name)
			}
			var fw *flushCountWriter
			got := r.serve(t, func(router httpx.Router) {
				router.Use(r.suite.StdMiddleware(func(next http.Handler) http.Handler {
					return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
						fw = &flushCountWriter{ResponseWriter: w}
						next.ServeHTTP(&unwrapOnlyWriter{w: fw}, req)
					})
				}))
				router.GET("/std-flush", func(ctx httpx.Context) error {
					f, ok := httpx.AsFlusher(ctx)
					if !ok {
						return httpx.NewInternalServerError("missing flusher")
					}
					ctx.Status(http.StatusAccepted)
					if err := f.Flush(); err != nil {
						return err
					}
					return ctx.Text(http.StatusOK, "flushed")
				})
			}, httptest.NewRequest(http.MethodGet, "http://example.com/std-flush", nil))
			if got.Body != "flushed" {
				t.Fatalf("body=%q, want %q", got.Body, "flushed")
			}
			// hertzx's in-process dispatch has no connection writer so Flush is a
			// buffered no-op; live network flushing is verified in conformance.
			if r.suite.Name != "hertzx" {
				if got.Status != http.StatusAccepted {
					t.Fatalf("status=%d, want 202", got.Status)
				}
				if fw == nil || fw.flushes == 0 {
					t.Fatalf("flush did not reach wrapped writer through Unwrap: flushes=%v", fw)
				}
			}
		})

		t.Run("PresetStatusPreserved", func(t *testing.T) {
			got := r.serve(t, func(router httpx.Router) {
				router.Use(func(next httpx.Handler) httpx.Handler {
					return func(ctx httpx.Context) error {
						ctx.Status(http.StatusAccepted)
						return next(ctx)
					}
				})
				router.Use(r.suite.StdMiddleware(func(http.Handler) http.Handler {
					return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
						_, _ = io.WriteString(w, "accepted")
					})
				}))
				router.GET("/std-preset-status", func(ctx httpx.Context) error {
					return ctx.Text(http.StatusOK, "unreached")
				})
			}, httptest.NewRequest(http.MethodGet, "http://example.com/std-preset-status", nil))
			if got.Status != http.StatusAccepted || got.Body != "accepted" {
				t.Fatalf("status=%d body=%q, want 202 %q", got.Status, got.Body, "accepted")
			}
		})

		t.Run("Informational1xxDoesNotCommit", func(t *testing.T) {
			var committedInHandler bool
			got := r.serve(t, func(router httpx.Router) {
				router.Use(r.suite.StdMiddleware(func(next http.Handler) http.Handler {
					return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
						next.ServeHTTP(&hintCapturingWriter{ResponseWriter: w}, req)
					})
				}))
				router.Use(r.suite.StdMiddleware(func(next http.Handler) http.Handler {
					return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
						w.WriteHeader(http.StatusEarlyHints)
						next.ServeHTTP(w, req)
					})
				}))
				router.GET("/std-103", func(ctx httpx.Context) error {
					committedInHandler = ctx.Committed()
					return ctx.Text(http.StatusOK, "done")
				})
			}, httptest.NewRequest(http.MethodGet, "http://example.com/std-103", nil))
			if committedInHandler {
				t.Fatal("103 latched ctx.Committed() = true before handler ran")
			}
			if got.Status != http.StatusOK || got.Body != "done" {
				t.Fatalf("status=%d body=%q, want 200 %q", got.Status, got.Body, "done")
			}
		})

		t.Run("PreCommittedIgnoresStatusAndHeaders", func(t *testing.T) {
			got := r.serve(t, func(router httpx.Router) {
				router.Use(func(next httpx.Handler) httpx.Handler {
					return func(ctx httpx.Context) error {
						if err := ctx.Text(http.StatusCreated, "first"); err != nil {
							return err
						}
						return next(ctx)
					}
				})
				router.Use(r.suite.StdMiddleware(func(http.Handler) http.Handler {
					return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
						w.Header().Set("X-Late", "1")
						w.WriteHeader(http.StatusTeapot)
						_, _ = io.WriteString(w, "-second")
					})
				}))
				router.GET("/std-late", func(ctx httpx.Context) error {
					return ctx.Text(http.StatusOK, "unreached")
				})
			}, httptest.NewRequest(http.MethodGet, "http://example.com/std-late", nil))
			if got.Status != http.StatusCreated || got.Body != "first-second" || got.Headers.Get("X-Late") != "" {
				t.Fatalf("status=%d body=%q X-Late=%q, want 201 %q and no X-Late",
					got.Status, got.Body, got.Headers.Get("X-Late"), "first-second")
			}
		})
	})

	t.Run("FileByteRangeAndAcceptEncodingPreserved", func(t *testing.T) {
		p := filepath.Join(t.TempDir(), "sample.txt")
		if err := os.WriteFile(p, []byte("0123456789"), 0o600); err != nil {
			t.Fatalf("write file: %v", err)
		}
		var acceptEncAfter string
		req := httptest.NewRequest(http.MethodGet, "http://example.com/range-file", nil)
		req.Header.Set("Range", "bytes=2-4")
		req.Header.Set("Accept-Encoding", "gzip")
		got := r.serve(t, func(router httpx.Router) {
			router.GET("/range-file", func(ctx httpx.Context) error {
				err := ctx.File(p)
				acceptEncAfter = ctx.Header("Accept-Encoding")
				return err
			})
		}, req)

		if got.Status != http.StatusPartialContent || got.Body != "234" {
			t.Fatalf("status=%d body=%q, want 206 %q", got.Status, got.Body, "234")
		}
		if !strings.HasPrefix(got.Headers.Get("Content-Range"), "bytes 2-4/") {
			t.Fatalf("Content-Range = %q, want bytes 2-4/...", got.Headers.Get("Content-Range"))
		}
		if acceptEncAfter != "gzip" {
			t.Fatalf("Accept-Encoding after File = %q, want %q", acceptEncAfter, "gzip")
		}
	})

	t.Run("BindJSONNilBodyReturns400", func(t *testing.T) {
		type payload struct {
			Name string `json:"name"`
		}
		req := httptest.NewRequest(http.MethodPost, "http://example.com/bind-json-nil", nil)
		req.Body = nil
		got := r.serveWith(t, Options{ErrorHandler: statusTextErrorHandler}, func(router httpx.Router) {
			router.POST("/bind-json-nil", func(ctx httpx.Context) error {
				var p payload
				return ctx.BindJSON(&p)
			})
		}, req)
		if got.Status != http.StatusBadRequest {
			t.Fatalf("status=%d body=%q, want 400", got.Status, got.Body)
		}
	})

	t.Run("BindJSONEmptyBodyReturnsEOF", func(t *testing.T) {
		type payload struct {
			Name string `json:"name"`
		}
		var bindErr error
		req := httptest.NewRequest(http.MethodPost, "http://example.com/bind-json-empty", strings.NewReader(""))
		req.Header.Set("Content-Type", "application/json")
		got := r.serveWith(t, Options{ErrorHandler: statusTextErrorHandler}, func(router httpx.Router) {
			router.POST("/bind-json-empty", func(ctx httpx.Context) error {
				var p payload
				bindErr = ctx.BindJSON(&p)
				return bindErr
			})
		}, req)
		if got.Status != http.StatusBadRequest {
			t.Fatalf("status=%d body=%q, want 400", got.Status, got.Body)
		}
		if !errors.Is(bindErr, io.EOF) {
			t.Fatalf("BindJSON error = %v, want errors.Is(err, io.EOF)", bindErr)
		}
	})

	t.Run("BindFormUnbalancedBracketsNoPanic", func(t *testing.T) {
		type formIn struct {
			Name string            `form:"name"`
			A    map[string]string `form:"a"`
		}
		for _, body := range []string{"a[=1", "a]=1"} {
			req := httptest.NewRequest(http.MethodPost, "http://example.com/bind-form-bad", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			got := r.serveWith(t, Options{ErrorHandler: statusTextErrorHandler}, func(router httpx.Router) {
				router.POST("/bind-form-bad", func(ctx httpx.Context) error {
					var f formIn
					if err := ctx.BindForm(&f); err != nil {
						return err
					}
					return ctx.NoContent(http.StatusNoContent)
				})
			}, req)
			if got.Status != http.StatusBadRequest && got.Status != http.StatusNoContent {
				t.Fatalf("body %q: status=%d resp=%q, want 400 or 204", body, got.Status, got.Body)
			}
			if (r.suite.Name == "stdx" || r.suite.Name == "echox" || r.suite.Name == "hertzx") && got.Status != http.StatusBadRequest {
				t.Fatalf("body %q: status=%d resp=%q, want 400 on %s", body, got.Status, got.Body, r.suite.Name)
			}
		}
	})

	t.Run("JoinedGroupWildcardMustBeTerminalPanics", func(t *testing.T) {
		engine := r.suite.NewEngine(t, Options{})
		g := engine.Group("/files/*a")
		var panicked bool
		func() {
			defer func() {
				if recover() != nil {
					panicked = true
				}
			}()
			g.GET("/more", func(ctx httpx.Context) error { return ctx.NoContent(http.StatusNoContent) })
		}()
		if !panicked {
			t.Fatal("Group(\"/files/*a\").GET(\"/more\", ...) did not panic")
		}
	})
}
