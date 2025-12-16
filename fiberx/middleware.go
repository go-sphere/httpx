package fiberx

import (
	"fmt"

	"github.com/go-sphere/httpx"
	"github.com/gofiber/fiber/v3"
)

func toMiddleware(middleware httpx.Middleware, errorHandler httpx.ErrorHandler) fiber.Handler {
	return func(ctx fiber.Ctx) error {
		fc := &fiberContext{
			ctx:          ctx,
			errorHandler: errorHandler,
		}
		return middleware(fc)
	}
}

func toMiddlewares(middlewares []httpx.Middleware, errorHandler httpx.ErrorHandler) []any {
	fMid := make([]any, len(middlewares))
	for i, m := range middlewares {
		fMid[i] = toMiddleware(m, errorHandler)
	}
	return fMid
}

func MiddlewareAdapter(middleware fiber.Handler) httpx.Middleware {
	return func(ctx httpx.Context) error {
		fc, ok := ctx.(*fiberContext)
		if !ok {
			return fmt.Errorf("invalid context type: %T", ctx)
		}
		return middleware(fc.ctx)
	}
}
