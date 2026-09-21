package stdx

import (
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/go-sphere/httpx"
)

// The flush path against writers that cannot flush, or can only through
// Unwrap. The shared suite covers the plain case through AdaptStdMiddleware;
// these pin the wrapper's own behavior and the realistic TimeoutHandler shape.

// plainWriter is a ResponseWriter with the three required methods and nothing
// else: no Flush, no Unwrap.
type plainWriter struct {
	header http.Header
	status int
	body   []byte
}

func (w *plainWriter) Header() http.Header         { return w.header }
func (w *plainWriter) WriteHeader(code int)        { w.status = code }
func (w *plainWriter) Write(p []byte) (int, error) { w.body = append(w.body, p...); return len(p), nil }

// unwrapOnlyWriter hides a flushable writer behind Unwrap, the shape a wrapper
// written for Go 1.20+ takes.
type unwrapOnlyWriter struct {
	*plainWriter
	flushes int
}

func (w *unwrapOnlyWriter) Unwrap() http.ResponseWriter { return flushCounter{w} }

type flushCounter struct{ w *unwrapOnlyWriter }

func (f flushCounter) Header() http.Header         { return f.w.header }
func (f flushCounter) WriteHeader(code int)        { f.w.status = code }
func (f flushCounter) Write(p []byte) (int, error) { return f.w.plainWriter.Write(p) }
func (f flushCounter) Flush()                      { f.w.flushes++ }

func streamTwo(ctx httpx.Context) error {
	s, ok := httpx.AsStreamer(ctx)
	if !ok {
		return httpx.NewInternalServerError("no Streamer")
	}
	return s.Stream(http.StatusOK, "text/event-stream", func(w io.Writer) error {
		if _, err := io.WriteString(w, "data: one\n\n"); err != nil {
			return err
		}
		_, err := io.WriteString(w, "data: two\n\n")
		return err
	})
}

func TestFlushOnWriterWithoutFlusher(t *testing.T) {
	t.Run("FlushReportsNothing", func(t *testing.T) {
		engine, r := newTestEngine(t)
		var flushErr error
		r.GET("/f", func(ctx httpx.Context) error {
			fl, ok := httpx.AsFlusher(ctx)
			if !ok {
				return httpx.NewInternalServerError("no Flusher")
			}
			flushErr = fl.Flush()
			return ctx.Text(http.StatusOK, "after")
		})
		w := &plainWriter{header: make(http.Header)}
		engine.ServeHTTP(w, getReq("/f"))
		if flushErr != nil {
			t.Fatalf("Flush = %v, want nil: an unsupported flush is a no-op by contract", flushErr)
		}
		if w.status != http.StatusOK || string(w.body) != "after" {
			t.Fatalf("status = %d, body = %q", w.status, w.body)
		}
	})

	t.Run("StreamRunsToCompletion", func(t *testing.T) {
		engine, r := newTestEngine(t)
		r.GET("/s", streamTwo)
		w := &plainWriter{header: make(http.Header)}
		engine.ServeHTTP(w, getReq("/s"))
		if w.status != http.StatusOK {
			t.Fatalf("status = %d, want 200", w.status)
		}
		if got := string(w.body); got != "data: one\n\ndata: two\n\n" {
			t.Fatalf("body = %q: Stream stopped at a writer that cannot flush", got)
		}
		if ct := w.header.Get("Content-Type"); ct != "text/event-stream" {
			t.Fatalf("Content-Type = %q", ct)
		}
	})

	// http.TimeoutHandler is the standard library's own non-flushing writer,
	// and the realistic way a stream lands on one.
	t.Run("StreamInsideTimeoutHandler", func(t *testing.T) {
		engine, r := newTestEngine(t)
		r.Use(AdaptStdMiddleware(func(next http.Handler) http.Handler {
			return http.TimeoutHandler(next, time.Second, "timed out")
		}))
		r.GET("/s", streamTwo)
		rec := serve(engine, getReq("/s"))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %q", rec.Code, rec.Body.String())
		}
		if got := rec.Body.String(); got != "data: one\n\ndata: two\n\n" {
			t.Fatalf("body = %q: Stream stopped inside TimeoutHandler", got)
		}
	})
}

func TestFlushReachesWriterThroughUnwrap(t *testing.T) {
	engine, r := newTestEngine(t)
	r.GET("/s", streamTwo)
	w := &unwrapOnlyWriter{plainWriter: &plainWriter{header: make(http.Header)}}
	engine.ServeHTTP(w, getReq("/s"))
	if got := string(w.body); got != "data: one\n\ndata: two\n\n" {
		t.Fatalf("body = %q", got)
	}
	// One flush to commit the stream, one per write.
	if w.flushes != 3 {
		t.Fatalf("flushes = %d, want 3: the controller did not follow Unwrap", w.flushes)
	}
}
