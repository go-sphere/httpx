package fiberx

import (
	"fmt"

	"github.com/go-sphere/httpx"
	"github.com/gofiber/fiber/v3"
)

func adaptMiddleware(middleware httpx.Middleware, errorHandler httpx.ErrorHandler) any {
	return func(ctx fiber.Ctx) error {
		fc := &fiberContext{
			ctx:          ctx,
			errorHandler: errorHandler,
		}
		if err := middleware(fc); err != nil {
			if errorHandler != nil {
				errorHandler(fc, err)
			}
		}
		return nil
	}
}

func adaptMiddlewares(middlewares []httpx.Middleware, errorHandler httpx.ErrorHandler) []any {
	if len(middlewares) == 0 {
		return nil
	}
	fMid := make([]any, len(middlewares))
	for i, m := range middlewares {
		fMid[i] = adaptMiddleware(m, errorHandler)
	}
	return fMid
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
			return fmt.Errorf("invalid context type: %T", ctx)
		}
		return middleware(fc.ctx)
	}
}
