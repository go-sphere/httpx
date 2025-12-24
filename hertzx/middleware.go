package hertzx

import (
	"context"
	"errors"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/go-sphere/httpx"
)

func adaptMiddleware(middleware httpx.Middleware, errHandler ErrorHandler) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		fc := &hertzContext{
			ctx:     ctx,
			baseCtx: c,
		}
		if err := middleware(fc); err != nil {
			errHandler(c, ctx, err)
		}
	}
}

func adaptMiddlewares(middlewares []httpx.Middleware, errHandler ErrorHandler) []app.HandlerFunc {
	if len(middlewares) == 0 {
		return nil
	}
	gMid := make([]app.HandlerFunc, len(middlewares))
	for i, m := range middlewares {
		gMid[i] = adaptMiddleware(m, errHandler)
	}
	return gMid
}

func AdaptHertzMiddleware(middleware app.HandlerFunc) httpx.Middleware {
	return func(ctx httpx.Context) error {
		fc, ok := ctx.(*hertzContext)
		if !ok {
			return errors.New("AdaptHertzMiddleware: invalid context type")
		}
		middleware(fc.baseCtx, fc.ctx)
		return nil
	}
}
