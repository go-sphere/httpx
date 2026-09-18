package echox

import (
	"bufio"
	"errors"
	"net"
	"net/http"

	"github.com/go-sphere/httpx"
	"github.com/labstack/echo/v4"
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
		ec, ok := httpx.AsNativeContext[echo.Context](ctx)
		if !ok {
			return errors.New("AdaptStdMiddleware: invalid context type")
		}
		resp := ec.Response()
		origWriter := resp.Writer
		// Track writes that bypass echo.Response, including short circuits.
		writer := &commitRecorder{ResponseWriter: origWriter, resp: resp}
		var nextErr error
		inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ec.SetRequest(r)
			if w != http.ResponseWriter(writer) {
				resp.Writer = w
				defer func() { resp.Writer = origWriter }()
			}
			nextErr = ctx.Next()
		})
		middleware(inner).ServeHTTP(writer, ec.Request())
		return nextErr
	}
}

// commitRecorder reports writes on the bridged writer to echo's Response, so
// Committed also reflects responses produced by an adapted net/http middleware
// that never goes through echo's own write path.
type commitRecorder struct {
	http.ResponseWriter
	resp *echo.Response
}

func (c *commitRecorder) WriteHeader(code int) {
	if !c.resp.Committed && (code >= 200 || code == http.StatusSwitchingProtocols) {
		c.resp.Status = code
		c.resp.Committed = true
	}
	c.ResponseWriter.WriteHeader(code)
}

func (c *commitRecorder) Write(p []byte) (int, error) {
	if !c.resp.Committed {
		c.WriteHeader(http.StatusOK)
	}
	return c.ResponseWriter.Write(p)
}

func (c *commitRecorder) Unwrap() http.ResponseWriter {
	return c.ResponseWriter
}

func (c *commitRecorder) FlushError() error {
	if !c.resp.Committed {
		c.WriteHeader(http.StatusOK)
	}
	return http.NewResponseController(c.ResponseWriter).Flush()
}

func (c *commitRecorder) Flush() {
	_ = c.FlushError()
}

func (c *commitRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return http.NewResponseController(c.ResponseWriter).Hijack()
}
