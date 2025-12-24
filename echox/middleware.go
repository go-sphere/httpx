package echox

import (
	"errors"

	"github.com/go-sphere/httpx"
	"github.com/labstack/echo/v4"
)

func adaptMiddleware(middleware httpx.Middleware) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(ec echo.Context) error {
			ctx := newEchoContext(ec)
			ctx.next = next
			return middleware(ctx)
		}
	}
}

func adaptMiddlewares(middlewares []httpx.Middleware) []echo.MiddlewareFunc {
	if len(middlewares) == 0 {
		return nil
	}
	out := make([]echo.MiddlewareFunc, len(middlewares))
	for i, m := range middlewares {
		out[i] = adaptMiddleware(m)
	}
	return out
}

func AdaptEchoMiddleware(middleware echo.MiddlewareFunc) httpx.Middleware {
	if middleware == nil {
		return func(ctx httpx.Context) error {
			return ctx.Next()
		}
	}
	return func(ctx httpx.Context) error {
		ec, ok := ctx.(*echoContext)
		if !ok {
			return errors.New("AdaptEchoMiddleware: invalid context type")
		}
		nextHandler := middleware(func(e echo.Context) error {
			return ec.Next()
		})
		if nextHandler == nil {
			return ec.Next()
		}
		return nextHandler(ec.ctx)
	}
}
