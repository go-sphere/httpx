package fiberx

import (
	"fmt"

	"github.com/go-sphere/httpx"
	"github.com/gofiber/fiber/v3"
)

type ErrorHandler func(ctx httpx.Context, err error)

func adaptMiddleware(middleware httpx.Middleware) fiber.Handler {
	return func(ctx fiber.Ctx) error {
		fc := &fiberContext{
			ctx: ctx,
		}
		middleware(fc)
		return nil
	}
}

func cloneMiddlewares(middlewares []httpx.Middleware, extra ...httpx.Middleware) []httpx.Middleware {
	out := make([]httpx.Middleware, len(middlewares)+len(extra))
	copy(out, middlewares)
	copy(out[len(middlewares):], extra)
	return out
}

func AdaptFiberMiddleware(middleware fiber.Handler, errorHandler ErrorHandler) httpx.Middleware {
	return func(ctx httpx.Context) {
		fc, ok := ctx.(*fiberContext)
		if !ok {
			panic(fmt.Sprintf("AdaptFiberMiddleware: invalid context type %T", ctx))
		}
		if fc.IsAborted() {
			return
		}
		err := middleware(fc.ctx)
		if err != nil && errorHandler != nil {
			errorHandler(fc, err)
		}
	}
}
