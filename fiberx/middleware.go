package fiberx

import (
	"errors"

	"github.com/go-sphere/httpx"
	"github.com/gofiber/fiber/v3"
)

func adaptMiddleware(middleware httpx.Middleware, errHandler httpx.ErrorHandler) fiber.Handler {
	return func(ctx fiber.Ctx) error {
		fc := newFiberContext(ctx)
		return handleFiberError(fc, middleware(fc), errHandler)
	}
}

func cloneMiddlewares(middlewares []httpx.Middleware, extra ...httpx.Middleware) []httpx.Middleware {
	out := make([]httpx.Middleware, len(middlewares)+len(extra))
	copy(out, middlewares)
	copy(out[len(middlewares):], extra)
	return out
}

// AdaptFiberMiddleware wraps a native fiber middleware as httpx.Middleware.
// The httpx.Context is resolved through AsNativeContext so decorated contexts
// keep working.
func AdaptFiberMiddleware(middleware fiber.Handler) httpx.Middleware {
	return func(ctx httpx.Context) error {
		fc, ok := httpx.AsNativeContext[fiber.Ctx](ctx)
		if !ok {
			return errors.New("AdaptFiberMiddleware: fiber context type error")
		}
		return middleware(fc)
	}
}
