package echox

import (
	"github.com/go-sphere/httpx"
	"github.com/labstack/echo/v4"
)

func adaptMiddleware(middleware httpx.Middleware) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(ec echo.Context) error {
			ctx := newEchoContext(ec)
			ctx.next = next
			middleware(ctx)
			return ctx.err
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
		return func(ctx httpx.Context) {
			ctx.Next()
		}
	}
	return func(ctx httpx.Context) {
		ec, ok := ctx.(*echoContext)
		if !ok {
			panic("AdaptEchoMiddleware: invalid context type")
		}
		nextHandler := middleware(func(e echo.Context) error {
			ec.Next()
			return ec.err
		})
		if nextHandler == nil {
			ec.Next()
			return
		}
		if err := nextHandler(ec.ctx); err != nil && ec.err == nil {
			ec.err = err
		}
	}
}
