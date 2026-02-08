package fiberx

import (
	"errors"

	"github.com/go-sphere/httpx"
	"github.com/gofiber/fiber/v3"
)

func adaptMiddleware(middleware httpx.Middleware) fiber.Handler {
	return func(ctx fiber.Ctx) error {
		fc := newFiberContext(ctx)
		// Return error directly to fiber's error handling system
		return middleware(fc)
	}
}

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
			return errors.New("AdaptGinMiddleware: fiber context type error")
		}
		return middleware(fc.ctx)
	}
}
