package hertzx

import (
	"context"
	"fmt"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/go-sphere/httpx"
)

func toMiddleware(middleware httpx.Middleware, errorHandler httpx.ErrorHandler) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		fc := &hertzContext{
			ctx:          ctx,
			baseCtx:      c,
			errorHandler: errorHandler,
		}
		err := middleware(fc)
		if err != nil {
			errorHandler(fc, err)
		}
	}
}

func toMiddlewares(middlewares []httpx.Middleware, errorHandler httpx.ErrorHandler) []app.HandlerFunc {
	if len(middlewares) == 0 {
		return nil
	}
	gMid := make([]app.HandlerFunc, len(middlewares))
	for i, m := range middlewares {
		gMid[i] = toMiddleware(m, errorHandler)
	}
	return gMid
}

func MiddlewareAdapter(middleware app.HandlerFunc) httpx.Middleware {
	return func(ctx httpx.Context) error {
		fc, ok := ctx.(*hertzContext)
		if !ok {
			return fmt.Errorf("invalid context type: %T", ctx)
		}
		middleware(fc.baseCtx, fc.ctx)
		return nil
	}
}
