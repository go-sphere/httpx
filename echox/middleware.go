package echox

import (
	"errors"

	"github.com/go-sphere/httpx"
	"github.com/labstack/echo/v4"
)

func adaptMiddleware(middleware httpx.Middleware, errHandler httpx.ErrorHandler) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(ec echo.Context) error {
			ctx := newEchoContext(ec)
			ctx.next = next
			err := middleware(ctx)
			if err != nil && errHandler != nil {
				if !ec.Response().Committed {
					errHandler(ctx, err)
				}
				return nil
			}
			return err
		}
	}
}

func adaptMiddlewares(middlewares []httpx.Middleware, errHandler httpx.ErrorHandler) []echo.MiddlewareFunc {
	if len(middlewares) == 0 {
		return nil
	}
	out := make([]echo.MiddlewareFunc, len(middlewares))
	for i, m := range middlewares {
		out[i] = adaptMiddleware(m, errHandler)
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
