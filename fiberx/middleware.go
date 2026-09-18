package fiberx

import (
	"errors"
	"io"

	"github.com/go-sphere/httpx"
	"github.com/gofiber/fiber/v3"
)

func adaptMiddleware(middleware httpx.Middleware, errHandler httpx.ErrorHandler) fiber.Handler {
	return func(ctx fiber.Ctx) error {
		fc := &fiberMiddlewareContext{contextBase: newFiberContext(ctx)}
		return handleFiberError(ctx, fc, middleware(fc), errHandler)
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

// contextBase avoids a field-name collision with Context().
type contextBase = httpx.Context

type fiberMiddlewareContext struct {
	contextBase
	called bool
}

func (c *fiberMiddlewareContext) Next() error {
	if c.called {
		return nil
	}
	c.called = true
	return c.contextBase.Next()
}
func (c *fiberMiddlewareContext) NativeContext() any {
	native, _ := httpx.AsNativeContext[fiber.Ctx](c.contextBase)
	return native
}
func (c *fiberMiddlewareContext) Stream(code int, contentType string, fn func(io.Writer) error) error {
	s, ok := httpx.AsStreamer(c.contextBase)
	if !ok {
		return httpx.ErrStreamerNotSupported
	}
	return s.Stream(code, contentType, fn)
}
