package echox

import (
	"errors"
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
		var nextErr error
		inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ec.SetRequest(r)
			if w != origWriter {
				resp.Writer = w
				defer func() { resp.Writer = origWriter }()
			}
			nextErr = ctx.Next()
		})
		// The raw underlying writer is handed to the middleware; downstream
		// echo writes are routed through whatever the middleware wraps it
		// with. Short-circuit responses bypass echo's Committed tracking,
		// which is safe because nothing runs after a short circuit.
		middleware(inner).ServeHTTP(origWriter, ec.Request())
		return nextErr
	}
}
