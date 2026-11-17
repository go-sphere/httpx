package httpxtest

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/go-sphere/httpx"
)

func init() {
	register("StdBridge", casesStdBridge)
}

// The StdBridge cases pin the seam between httpx and plain net/http code: a
// handler mounted with HandleStd and a middleware adapted with
// AdaptStdMiddleware see the response the httpx chain has built so far, and
// write into it under the same rules as a Responder.
func casesStdBridge(t *testing.T, r runner) {
	// A mounted handler writing into a committed response follows net/http:
	// its status and headers are dropped, its bytes append.
	t.Run("HandleStdAfterCommitAppends", func(t *testing.T) {
		got := r.serve(t, func(router httpx.Router) {
			router.Use(func(next httpx.Handler) httpx.Handler {
				return func(ctx httpx.Context) error {
					if err := ctx.Text(http.StatusCreated, "a"); err != nil {
						return err
					}
					return next(ctx)
				}
			})
			mountStd(t, r, router, "/bridge/late", func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("X-Late", "1")
				w.WriteHeader(http.StatusAccepted)
				_, _ = io.WriteString(w, "b")
			})
		}, httptest.NewRequest(http.MethodGet, "http://example.com/bridge/late", nil))
		if got.Status != http.StatusCreated || got.Body != "ab" || got.Headers.Get("X-Late") != "" {
			t.Fatalf("status=%d body=%q X-Late=%q, want 201 %q and no X-Late", got.Status, got.Body, got.Headers.Get("X-Late"), "ab")
		}
	})

	// A mounted handler sees the headers the chain set, and Header().Set
	// replaces one rather than adding a second value.
	t.Run("HandleStdSeesAndReplacesHeaders", func(t *testing.T) {
		got := r.serve(t, func(router httpx.Router) {
			router.Use(func(next httpx.Handler) httpx.Handler {
				return func(ctx httpx.Context) error {
					ctx.SetHeader("Cache-Control", "from-chain")
					return next(ctx)
				}
			})
			mountStd(t, r, router, "/bridge/headers", func(w http.ResponseWriter, _ *http.Request) {
				seen := w.Header().Get("Cache-Control")
				w.Header().Set("Cache-Control", "from-std")
				_, _ = io.WriteString(w, seen)
			})
		}, httptest.NewRequest(http.MethodGet, "http://example.com/bridge/headers", nil))
		if got.Body != "from-chain" {
			t.Errorf("the mounted handler saw Cache-Control %q, want %q", got.Body, "from-chain")
		}
		if values := got.Headers.Values("Cache-Control"); len(values) != 1 || values[0] != "from-std" {
			t.Errorf("Cache-Control = %q, want only %q", values, "from-std")
		}
	})

	// An adapter that flushes lets a mounted handler flush too: an SSE handler
	// or httputil.ReverseProxy probes its writer for http.Flusher.
	t.Run("HandleStdWriterFlushes", func(t *testing.T) {
		if !r.suite.Caps.Flusher {
			r.skipf(t, "%s: Caps.Flusher is not declared", r.suite.Name)
		}
		got := r.serve(t, func(router httpx.Router) {
			mountStd(t, r, router, "/bridge/flush", func(w http.ResponseWriter, _ *http.Request) {
				f, ok := w.(http.Flusher)
				if ok {
					f.Flush()
				}
				_, _ = io.WriteString(w, strconv.FormatBool(ok))
			})
		}, httptest.NewRequest(http.MethodGet, "http://example.com/bridge/flush", nil))
		if got.Body != "true" {
			t.Fatalf("the mounted handler's writer is not an http.Flusher (body %q)", got.Body)
		}
	})

	if r.suite.StdMiddleware == nil {
		t.Run("StdMiddleware", func(t *testing.T) {
			r.skipf(t, "%s: no StdMiddleware hook declared", r.suite.Name)
		})
		return
	}

	// A net/http middleware that stops the chain without writing leaves the
	// response to the layers above it: an error one of them returns is still
	// rendered.
	t.Run("StdMiddlewareShortCircuitThenOuterError", func(t *testing.T) {
		got := r.serveWith(t, Options{ErrorHandler: statusTextErrorHandler}, func(router httpx.Router) {
			router.Use(func(next httpx.Handler) httpx.Handler {
				return func(ctx httpx.Context) error {
					if err := next(ctx); err != nil {
						return err
					}
					if !ctx.Committed() {
						return httpx.NewForbiddenError("nothing answered")
					}
					return nil
				}
			})
			router.Use(r.suite.StdMiddleware(func(http.Handler) http.Handler {
				return http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
			}))
			router.GET("/bridge/stopped", func(ctx httpx.Context) error {
				return ctx.Text(http.StatusOK, "handler")
			})
		}, httptest.NewRequest(http.MethodGet, "http://example.com/bridge/stopped", nil))
		if got.Status != http.StatusForbidden || got.Body != "err:403" {
			t.Fatalf("status=%d body=%q, want 403 %q", got.Status, got.Body, "err:403")
		}
	})

	// WriteHeader commits a net/http response, so a middleware that answers
	// with a status alone has answered: an error a layer above returns
	// afterwards is not rendered over it.
	t.Run("StdMiddlewareWriteHeaderOnlyCommits", func(t *testing.T) {
		got := r.serveWith(t, Options{ErrorHandler: statusTextErrorHandler}, func(router httpx.Router) {
			router.Use(func(next httpx.Handler) httpx.Handler {
				return func(ctx httpx.Context) error {
					_ = next(ctx)
					return httpx.NewForbiddenError("outer")
				}
			})
			router.Use(r.suite.StdMiddleware(func(http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusUnauthorized)
				})
			}))
			router.GET("/bridge/status-only", func(ctx httpx.Context) error {
				return ctx.Text(http.StatusOK, "handler")
			})
		}, httptest.NewRequest(http.MethodGet, "http://example.com/bridge/status-only", nil))
		if got.Status != http.StatusUnauthorized || got.Body != "" {
			t.Fatalf("status=%d body=%q, want 401 and no body", got.Status, got.Body)
		}
	})

	// Header changes a net/http middleware stages before calling next survive
	// when the chain leaves those keys alone, and a key it deletes is gone.
	t.Run("StdMiddlewareHeaderChangesSurvive", func(t *testing.T) {
		got := r.serve(t, func(router httpx.Router) {
			router.Use(func(next httpx.Handler) httpx.Handler {
				return func(ctx httpx.Context) error {
					ctx.SetHeader("X-Remove", "chain")
					return next(ctx)
				}
			})
			router.Use(r.suite.StdMiddleware(func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
					w.Header().Set("X-Staged", "std")
					w.Header().Del("X-Remove")
					next.ServeHTTP(w, req)
				})
			}))
			router.GET("/bridge/staged", func(ctx httpx.Context) error {
				return ctx.Text(http.StatusOK, "ok")
			})
		}, httptest.NewRequest(http.MethodGet, "http://example.com/bridge/staged", nil))
		if got.Headers.Get("X-Staged") != "std" {
			t.Errorf("X-Staged = %q, want %q", got.Headers.Get("X-Staged"), "std")
		}
		if values, ok := got.Headers["X-Remove"]; ok {
			t.Errorf("X-Remove = %q, want it deleted by the std middleware", values)
		}
	})

	// A middleware may wrap the writer with a header map of its own that it
	// copies out on WriteHeader, as buffering middleware does. Headers the
	// chain below sets reach that map, so the middleware observes them and
	// they reach the response.
	t.Run("StdMiddlewareWriterWithOwnHeaderMap", func(t *testing.T) {
		var observed string
		got := r.serve(t, func(router httpx.Router) {
			router.Use(r.suite.StdMiddleware(func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
					wrapped := &ownHeaderWriter{ResponseWriter: w, header: make(http.Header)}
					next.ServeHTTP(wrapped, req)
					observed = wrapped.Header().Get("X-Downstream")
				})
			}))
			router.GET("/bridge/own-header", func(ctx httpx.Context) error {
				ctx.SetHeader("X-Downstream", "value")
				return ctx.NoContent(http.StatusNoContent)
			})
		}, httptest.NewRequest(http.MethodGet, "http://example.com/bridge/own-header", nil))
		if observed != "value" {
			t.Errorf("the wrapping writer observed X-Downstream %q, want %q", observed, "value")
		}
		if got.Headers.Get("X-Downstream") != "value" {
			t.Errorf("X-Downstream = %q on the response, want %q", got.Headers.Get("X-Downstream"), "value")
		}
	})

	// Passing a response through a net/http middleware adds nothing to it: a
	// 204 has no Content-Type before and after.
	t.Run("StdMiddlewareAddsNoContentType", func(t *testing.T) {
		got := r.serve(t, func(router httpx.Router) {
			router.Use(r.suite.StdMiddleware(func(next http.Handler) http.Handler { return next }))
			router.GET("/bridge/nocontent", func(ctx httpx.Context) error {
				return ctx.NoContent(http.StatusNoContent)
			})
		}, httptest.NewRequest(http.MethodGet, "http://example.com/bridge/nocontent", nil))
		if got.Status != http.StatusNoContent {
			t.Fatalf("status = %d, want 204", got.Status)
		}
		if ct, ok := got.Headers["Content-Type"]; ok {
			t.Fatalf("Content-Type = %q on a 204 nothing labelled", ct)
		}
	})
}

// mountStd registers h with httpx.MountStd, skipping the case when the router
// cannot mount net/http handlers.
func mountStd(t *testing.T, r runner, router httpx.Router, path string, h http.HandlerFunc) {
	t.Helper()
	if !httpx.MountStd(router, http.MethodGet, path, h) {
		r.skipf(t, "%s: router does not implement httpx.StdHandlerMounter", r.suite.Name)
	}
}

// ownHeaderWriter keeps its own header map and copies it to the wrapped
// writer on WriteHeader, as buffering net/http middleware does.
type ownHeaderWriter struct {
	http.ResponseWriter
	header http.Header
}

func (w *ownHeaderWriter) Header() http.Header { return w.header }

func (w *ownHeaderWriter) WriteHeader(status int) {
	for k, v := range w.header {
		w.ResponseWriter.Header()[k] = v
	}
	w.ResponseWriter.WriteHeader(status)
}
