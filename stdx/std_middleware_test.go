package stdx

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-sphere/httpx"
)

// upperWriter is the shape of a body-transforming middleware writer (gzip,
// minify): downstream writes must go through it, and only while it is
// installed.
type upperWriter struct{ http.ResponseWriter }

func (u upperWriter) Write(p []byte) (int, error) {
	return u.ResponseWriter.Write(bytes.ToUpper(p))
}

func TestAdaptStdMiddleware(t *testing.T) {
	t.Run("NilMiddlewarePassesThrough", func(t *testing.T) {
		var tr trace
		engine, r := newTestEngine(t)
		r.Use(AdaptStdMiddleware(nil))
		r.GET("/x", tr.leaf("leaf"))
		if rec := serve(engine, getReq("/x")); rec.Code != http.StatusNoContent || tr.String() != "leaf" {
			t.Fatalf("status = %d, chain = %q", rec.Code, tr.String())
		}
	})

	t.Run("RequestMutationsReachTheHandler", func(t *testing.T) {
		engine, r := newTestEngine(t)
		r.Use(AdaptStdMiddleware(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				req = req.WithContext(context.WithValue(req.Context(), ctxKey{}, "from-std"))
				req.Header.Set("X-Injected", "1")
				next.ServeHTTP(w, req)
			})
		}))
		var value any
		var header string
		r.GET("/x", func(ctx httpx.Context) error {
			value = ctx.Context().Value(ctxKey{})
			header = ctx.Header("X-Injected")
			return nil
		})
		serve(engine, getReq("/x"))
		if value != "from-std" || header != "1" {
			t.Fatalf("value=%v header=%q", value, header)
		}
	})

	t.Run("WrappedWriterAppliesOnlyInsideTheMiddleware", func(t *testing.T) {
		engine, r := newTestEngine(t)
		// Outer layer writes after the adapted middleware has returned: by
		// then the wrapper must be gone again.
		r.Use(func(ctx httpx.Context) error {
			if err := ctx.Next(); err != nil {
				return err
			}
			return ctx.Bytes(http.StatusOK, []byte("tail"), "text/plain")
		})
		r.Use(AdaptStdMiddleware(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				next.ServeHTTP(upperWriter{w}, req)
			})
		}))
		r.GET("/x", func(ctx httpx.Context) error { return ctx.Text(http.StatusOK, "body") })
		rec := serve(engine, getReq("/x"))
		if rec.Body.String() != "BODYtail" {
			t.Fatalf("body = %q, want %q", rec.Body.String(), "BODYtail")
		}
	})

	t.Run("ShortCircuitCountsAsCommitted", func(t *testing.T) {
		var tr trace
		engine, r := newTestEngine(t)
		var status int
		r.Use(func(ctx httpx.Context) error {
			err := ctx.Next()
			status = ctx.StatusCode()
			return err
		})
		r.Use(AdaptStdMiddleware(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte("denied"))
			})
		}))
		r.GET("/x", tr.leaf("leaf"))
		rec := serve(engine, getReq("/x"))
		if rec.Code != http.StatusUnauthorized || rec.Body.String() != "denied" || tr.String() != "" {
			t.Fatalf("status = %d, body = %q, chain = %q", rec.Code, rec.Body.String(), tr.String())
		}
		if status != http.StatusUnauthorized {
			t.Fatalf("StatusCode after a std short-circuit = %d, want 401", status)
		}
	})

	t.Run("WriteWithoutWriteHeaderCommitsAs200", func(t *testing.T) {
		engine, r := newTestEngine(t)
		r.Use(AdaptStdMiddleware(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				_, _ = w.Write([]byte("direct"))
			})
		}))
		r.GET("/x", func(ctx httpx.Context) error { return errors.New("unreachable") })
		rec := serve(engine, getReq("/x"))
		if rec.Code != http.StatusOK || rec.Body.String() != "direct" {
			t.Fatalf("status = %d, body = %q", rec.Code, rec.Body.String())
		}
	})

	t.Run("DownstreamErrorIsReturnedThroughTheBridge", func(t *testing.T) {
		engine, r := newTestEngine(t)
		var seen error
		r.Use(func(ctx httpx.Context) error {
			seen = ctx.Next()
			return seen
		})
		r.Use(AdaptStdMiddleware(func(next http.Handler) http.Handler { return next }))
		r.GET("/x", func(ctx httpx.Context) error { return httpx.NewForbiddenError("no") })
		rec := serve(engine, getReq("/x"))
		if rec.Code != http.StatusForbidden || seen == nil {
			t.Fatalf("status = %d, seen = %v", rec.Code, seen)
		}
	})

	t.Run("FlushThroughTheBridgeCommits", func(t *testing.T) {
		engine, r := newTestEngine(t)
		r.Use(AdaptStdMiddleware(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				flusher, ok := w.(http.Flusher)
				if !ok {
					http.Error(w, "no flusher", http.StatusInternalServerError)
					return
				}
				flusher.Flush()
			})
		}))
		r.GET("/x", func(ctx httpx.Context) error { return errors.New("unreachable") })
		rec := serve(engine, getReq("/x"))
		if !rec.Flushed || rec.Code != http.StatusOK || rec.Body.Len() != 0 {
			t.Fatalf("flushed=%v status=%d body=%q", rec.Flushed, rec.Code, rec.Body.String())
		}
	})

	t.Run("ReadFromThroughTheBridgeCommits", func(t *testing.T) {
		engine, r := newTestEngine(t)
		r.Use(AdaptStdMiddleware(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				_, _ = io.Copy(w, strings.NewReader("copied"))
			})
		}))
		r.GET("/x", func(ctx httpx.Context) error { return errors.New("unreachable") })
		rec := serve(engine, getReq("/x"))
		if rec.Code != http.StatusOK || rec.Body.String() != "copied" {
			t.Fatalf("status = %d, body = %q", rec.Code, rec.Body.String())
		}
	})

	t.Run("HijackThroughTheBridgeCommits", func(t *testing.T) {
		engine, r := newTestEngine(t)
		var hijackErr error
		r.Use(AdaptStdMiddleware(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				conn, _, err := http.NewResponseController(w).Hijack()
				hijackErr = err
				if err == nil {
					_ = conn.Close()
				}
			})
		}))
		r.GET("/x", func(ctx httpx.Context) error { return errors.New("unreachable") })
		rec := &hijackRecorder{ResponseRecorder: httptest.NewRecorder()}
		engine.ServeHTTP(rec, getReq("/x"))
		if hijackErr != nil || !rec.hijacked || rec.Body.Len() != 0 {
			t.Fatalf("err=%v hijacked=%v body=%q", hijackErr, rec.hijacked, rec.Body.String())
		}
	})

	t.Run("ForeignContextIsRejected", func(t *testing.T) {
		mw := AdaptStdMiddleware(func(next http.Handler) http.Handler { return next })
		if err := mw(foreignContext{}); err == nil {
			t.Fatal("a context without this adapter's native hook was accepted")
		}
	})
}

// foreignContext satisfies httpx.Context (through the embedded nil interface,
// aliased because the interface has a method called Context) but is not
// backed by this adapter.
type foreignContext struct{ anyContext }

type anyContext = httpx.Context
