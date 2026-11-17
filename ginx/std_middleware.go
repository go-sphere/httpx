package ginx

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/go-sphere/httpx"
)

// AdaptStdMiddleware wraps a plain net/http middleware
// (func(http.Handler) http.Handler) as httpx.Middleware, giving access to the
// otel/chi/gzip ecosystem. Request mutations (including context values) and
// response-writer wrapping are propagated to the downstream httpx chain.
// If the middleware responds without calling the next handler, the chain is
// short-circuited. If it stops without writing anything, the response is left
// to the layers above it, so an error one of them returns is still rendered.
//
// The middleware writes with net/http semantics: WriteHeader commits the
// response, where gin's own writer only records the status for the first body
// write.
func AdaptStdMiddleware(middleware func(http.Handler) http.Handler) httpx.Middleware {
	if middleware == nil {
		return func(next httpx.Handler) httpx.Handler { return next }
	}
	return func(next httpx.Handler) httpx.Handler {
		return func(ctx httpx.Context) error {
			gc, ok := httpx.AsNativeContext[*gin.Context](ctx)
			if !ok {
				return errors.New("AdaptStdMiddleware: gin context type error")
			}
			var nextErr error
			sw := http.ResponseWriter(stdResponseWriter{gc.Writer})
			inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gc.Request = r
				if w != sw {
					old := gc.Writer
					gc.Writer = newStdWriterBridge(old, w)
					defer func() { gc.Writer = old }()
				}
				nextErr = next(ctx)
			})
			middleware(inner).ServeHTTP(sw, gc.Request)
			return nextErr
		}
	}
}

// stdResponseWriter is gin's writer as net/http code expects it: WriteHeader
// with a final status commits the response at once, so a middleware or mounted
// handler that answers with a status alone has answered, and Committed says
// so. gin's writer would hold the status until the first body write, leaving
// the response open to an error rendered by a layer above.
type stdResponseWriter struct {
	gin.ResponseWriter
}

func (w stdResponseWriter) WriteHeader(code int) {
	w.ResponseWriter.WriteHeader(code)
	// An informational status does not end the header phase in net/http.
	if code >= http.StatusOK || code == http.StatusSwitchingProtocols {
		w.WriteHeaderNow()
	}
}

// Unwrap lets http.ResponseController reach the connection's writer.
func (w stdResponseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

// stdWriterBridge exposes a (possibly wrapped) http.ResponseWriter through
// gin's ResponseWriter interface. Status writes are deferred like gin's own
// writer: the header is flushed on the first body write or WriteHeaderNow.
// The embedded original writer serves Hijack/CloseNotify/Pusher and records
// the status so gin's finalization sees it even when no body is written.
type stdWriterBridge struct {
	gin.ResponseWriter
	target      http.ResponseWriter
	status      int
	size        int
	wroteHeader bool
}

func newStdWriterBridge(orig gin.ResponseWriter, target http.ResponseWriter) *stdWriterBridge {
	status := orig.Status()
	if status == 0 {
		status = http.StatusOK
	}
	size := orig.Size()
	if size < 0 {
		size = 0
	}
	return &stdWriterBridge{
		ResponseWriter: orig,
		target:         target,
		status:         status,
		size:           size,
		wroteHeader:    orig.Written(),
	}
}

func (b *stdWriterBridge) Unwrap() http.ResponseWriter {
	return b.target
}

func (b *stdWriterBridge) Header() http.Header {
	return b.target.Header()
}

func (b *stdWriterBridge) WriteHeader(code int) {
	if b.wroteHeader || code <= 0 {
		return
	}
	if code >= 100 && code < 200 && code != http.StatusSwitchingProtocols {
		b.target.WriteHeader(code)
		return
	}
	b.status = code
	// Record on the original gin writer too (it defers the actual write)
	// so gin's finalization flushes the right status when the wrapped
	// writer never writes a body.
	b.ResponseWriter.WriteHeader(code)
}

func (b *stdWriterBridge) WriteHeaderNow() {
	if b.wroteHeader {
		return
	}
	b.wroteHeader = true
	b.target.WriteHeader(b.status)
}

func (b *stdWriterBridge) Write(p []byte) (int, error) {
	b.WriteHeaderNow()
	n, err := b.target.Write(p)
	b.size += n
	return n, err
}

func (b *stdWriterBridge) WriteString(s string) (int, error) {
	return b.Write([]byte(s))
}

func (b *stdWriterBridge) Status() int {
	return b.status
}

func (b *stdWriterBridge) Size() int {
	return b.size
}

func (b *stdWriterBridge) Written() bool {
	return b.wroteHeader || b.size > 0
}

func (b *stdWriterBridge) Flush() {
	_ = b.flush()
}

func (b *stdWriterBridge) flush() error {
	b.WriteHeaderNow()
	return flushHTTPWriter(b.target)
}

func flushHTTPWriter(w http.ResponseWriter) error {
	if w == nil {
		return nil
	}
	for cur := w; cur != nil; {
		if f, ok := cur.(http.Flusher); ok {
			f.Flush()
			return nil
		}
		u, ok := cur.(interface{ Unwrap() http.ResponseWriter })
		if !ok {
			break
		}
		cur = u.Unwrap()
	}
	if err := http.NewResponseController(w).Flush(); err != nil && !errors.Is(err, http.ErrNotSupported) {
		return err
	}
	return nil
}
