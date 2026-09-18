package stdx

import (
	"bufio"
	"errors"
	"io"
	"net"
	"net/http"

	"github.com/go-sphere/httpx"
)

// AdaptStdMiddleware wraps a plain net/http middleware
// (func(http.Handler) http.Handler) as httpx.Middleware, giving access to the
// otel/chi/gzip ecosystem. Request mutations (including context values) and
// response-writer wrapping are propagated to the downstream httpx chain. If
// the middleware responds without calling the next handler, the chain is
// short-circuited.
//
// On this adapter the middleware runs on the server's own writer and request,
// with no translation in either direction.
func AdaptStdMiddleware(middleware func(http.Handler) http.Handler) httpx.Middleware {
	if middleware == nil {
		return func(ctx httpx.Context) error {
			return ctx.Next()
		}
	}
	return func(ctx httpx.Context) error {
		native, ok := httpx.AsNativeContext[*Native](ctx)
		if !ok {
			return errors.New("AdaptStdMiddleware: invalid context type")
		}
		c := native.c
		// The middleware is handed a recorder over the *underlying* writer,
		// never over c.rw: a middleware that wraps what it is given and passes
		// the result downstream would otherwise close a cycle through c.rw.
		underlying := c.rw.ResponseWriter
		recorder := &commitRecorder{ResponseWriter: underlying, ctx: c}

		var nextErr error
		inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			c.req = r
			if w != http.ResponseWriter(recorder) {
				// The middleware wrapped the writer: downstream writes have to
				// go through it, and the wrapper is dropped again afterwards.
				c.rw.ResponseWriter = w
				defer func() { c.rw.ResponseWriter = underlying }()
			}
			nextErr = ctx.Next()
		})
		middleware(inner).ServeHTTP(recorder, c.req)
		return nextErr
	}
}

// commitRecorder reports writes made on the bridged writer to the context, so
// a response produced by an adapted net/http middleware — including one that
// short-circuits the chain — counts as committed.
type commitRecorder struct {
	http.ResponseWriter
	ctx *stdContext
}

func (c *commitRecorder) WriteHeader(code int) {
	c.ctx.rw.markWritten(code)
	c.ResponseWriter.WriteHeader(code)
}

func (c *commitRecorder) Write(p []byte) (int, error) {
	c.ctx.rw.markWritten(http.StatusOK)
	return c.ResponseWriter.Write(p)
}

func (c *commitRecorder) Unwrap() http.ResponseWriter { return c.ResponseWriter }

func (c *commitRecorder) Flush() {
	c.ctx.rw.markWritten(http.StatusOK)
	_ = http.NewResponseController(c.ResponseWriter).Flush()
}

func (c *commitRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	conn, rw, err := http.NewResponseController(c.ResponseWriter).Hijack()
	if err == nil {
		c.ctx.rw.markWritten(http.StatusSwitchingProtocols)
	}
	return conn, rw, err
}

func (c *commitRecorder) ReadFrom(r io.Reader) (int64, error) {
	c.ctx.rw.markWritten(http.StatusOK)
	if rf, ok := c.ResponseWriter.(io.ReaderFrom); ok {
		return rf.ReadFrom(r)
	}
	return io.Copy(c.ResponseWriter, r)
}
