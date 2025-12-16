package hertzx

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/go-sphere/httpx"
)

func adaptMiddleware(middleware httpx.Middleware) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		fc := &hertzContext{
			ctx:     ctx,
			baseCtx: c,
		}
		middleware(fc)
	}
}

func adaptMiddlewares(middlewares []httpx.Middleware) []app.HandlerFunc {
	if len(middlewares) == 0 {
		return nil
	}
	gMid := make([]app.HandlerFunc, len(middlewares))
	for i, m := range middlewares {
		gMid[i] = adaptMiddleware(m)
	}
	return gMid
}

func AdaptHertzMiddleware(middleware app.HandlerFunc) httpx.Middleware {
	return func(ctx httpx.Context) {
		fc, ok := ctx.(*hertzContext)
		if !ok {
			panic("AdaptHertzMiddleware: invalid context type")
		}
		middleware(fc.baseCtx, fc.ctx)
	}
}
