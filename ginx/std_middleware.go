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
// short-circuited.
func AdaptStdMiddleware(middleware func(http.Handler) http.Handler) httpx.Middleware {
	if middleware == nil {
		return func(ctx httpx.Context) error {
			return ctx.Next()
		}
	}
	return func(ctx httpx.Context) error {
		gc, ok := httpx.AsNativeContext[*gin.Context](ctx)
		if !ok {
			return errors.New("AdaptStdMiddleware: gin context type error")
		}
		var nextErr error
		ran := false
		inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ran = true
			gc.Request = r
			if w != http.ResponseWriter(gc.Writer) {
				old := gc.Writer
				gc.Writer = newStdWriterBridge(old, w)
				defer func() { gc.Writer = old }()
			}
			nextErr = ctx.Next()
		})
		middleware(inner).ServeHTTP(gc.Writer, gc.Request)
		if !ran && !gc.IsAborted() {
			gc.Abort()
		}
		return nextErr
	}
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
	return &stdWriterBridge{
		ResponseWriter: orig,
		target:         target,
		status:         http.StatusOK,
	}
}

func (b *stdWriterBridge) Header() http.Header {
	return b.target.Header()
}

func (b *stdWriterBridge) WriteHeader(code int) {
	if code > 0 && !b.wroteHeader {
		b.status = code
		// Record on the original gin writer too (it defers the actual write)
		// so gin's finalization flushes the right status when the wrapped
		// writer never writes a body.
		b.ResponseWriter.WriteHeader(code)
	}
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
	if f, ok := b.target.(http.Flusher); ok {
		b.WriteHeaderNow()
		f.Flush()
	}
}
