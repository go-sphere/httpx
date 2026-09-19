package echox

import (
	"errors"

	"github.com/go-sphere/httpx"
	"github.com/labstack/echo/v4"
)

// adaptMiddleware takes the engine's wildcard table because a middleware
// registered with Use runs on echo's own chain, ahead of the route handler and
// shared by every route in the scope: it has to resolve a named wildcard
// through the same table the handler will, or FullPath would read differently
// either side of Next.
func adaptMiddleware(middleware httpx.Middleware, errHandler httpx.ErrorHandler, wildcards *wildcardTable) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(ec echo.Context) error {
			ctx := newEchoContext(ec, wildcards)
			ctx.next = next
			err := middleware(ctx)
			if err != nil && errHandler != nil {
				if ec.Response().Committed {
					// Nothing may write to a committed response, but the
					// error still belongs on echo's error path so logging
					// middleware sees it instead of it vanishing here.
					return err
				}
				// The error may be echo's own (a middleware whose Next fell
				// through to an unmatched path): normalize it so the configured
				// handler sees 404/405 rather than an unclassified 500.
				errHandler(ctx, normalizeEchoError(err))
				// The error handler may have rendered nothing: commit here,
				// because no layer below runs and echo does not commit on its
				// own. See commitErrorStatus for why the status cannot stay 200.
				commitErrorStatus(ec.Response(), err)
				return nil
			}
			return err
		}
	}
}

func adaptMiddlewares(middlewares []httpx.Middleware, errHandler httpx.ErrorHandler, wildcards *wildcardTable) []echo.MiddlewareFunc {
	if len(middlewares) == 0 {
		return nil
	}
	out := make([]echo.MiddlewareFunc, len(middlewares))
	for i, m := range middlewares {
		out[i] = adaptMiddleware(m, errHandler, wildcards)
	}
	return out
}

// AdaptEchoMiddleware wraps a native echo middleware as httpx.Middleware.
// The httpx.Context is resolved through AsNativeContext so decorated contexts
// keep working.
func AdaptEchoMiddleware(middleware echo.MiddlewareFunc) httpx.Middleware {
	if middleware == nil {
		return func(ctx httpx.Context) error {
			return ctx.Next()
		}
	}
	return func(ctx httpx.Context) error {
		ec, ok := httpx.AsNativeContext[echo.Context](ctx)
		if !ok {
			return errors.New("AdaptEchoMiddleware: invalid context type")
		}
		nextHandler := middleware(func(e echo.Context) error {
			return ctx.Next()
		})
		if nextHandler == nil {
			return ctx.Next()
		}
		return nextHandler(ec)
	}
}
