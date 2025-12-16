package fiberx

import (
	"fmt"

	"github.com/go-sphere/httpx"
	"github.com/gofiber/fiber/v3"
)

func cloneMiddlewares(middlewares []httpx.Middleware, extra ...httpx.Middleware) []httpx.Middleware {
	out := make([]httpx.Middleware, len(middlewares)+len(extra))
	copy(out, middlewares)
	copy(out[len(middlewares):], extra)
	return out
}

func AdaptFiberMiddleware(middleware fiber.Handler) httpx.Middleware {
	return func(ctx httpx.Context) error {
		fc, ok := ctx.(*fiberContext)
		if !ok {
			return fmt.Errorf("invalid context type: %T", ctx)
		}
		return middleware(fc.ctx)
	}
}
