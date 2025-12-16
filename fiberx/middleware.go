package fiberx

import (
	"fmt"

	"github.com/go-sphere/httpx"
	"github.com/gofiber/fiber/v3"
)

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

func AdaptFiberMiddleware(middleware fiber.Handler) httpx.Middleware {
	return func(ctx httpx.Context) {
		fc, ok := ctx.(*fiberContext)
		if !ok {
			panic(fmt.Sprintf("AdaptFiberMiddleware: invalid context type %T", ctx))
		}
		_ = middleware(fc.ctx)
	}
}
