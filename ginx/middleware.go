package ginx

import (
	"errors"

	"github.com/gin-gonic/gin"
	"github.com/go-sphere/httpx"
)

func adaptMiddleware(middleware httpx.Middleware, errHandler ErrorHandler) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		fc := &ginContext{
			ctx: ctx,
		}
		if err := middleware(fc); err != nil {
			errHandler(ctx, err)
			if !ctx.IsAborted() {
				ctx.Abort()
			}
			return
		}

		if !fc.nextCalled {
			ctx.Abort()
		}
	}
}

func adaptMiddlewares(middlewares []httpx.Middleware, errHandler ErrorHandler) []gin.HandlerFunc {
	if len(middlewares) == 0 {
		return nil
	}
	gMid := make([]gin.HandlerFunc, len(middlewares))
	for i, m := range middlewares {
		gMid[i] = adaptMiddleware(m, errHandler)
	}
	return gMid
}

func AdaptGinMiddleware(middleware gin.HandlerFunc) httpx.Middleware {
	return func(ctx httpx.Context) error {
		fc, ok := ctx.(*ginContext)
		if !ok {
			return errors.New("AdaptGinMiddleware: gin context type error")
		}
		middleware(fc.ctx)
		return nil
	}
}
