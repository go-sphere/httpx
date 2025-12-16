package ginx

import (
	"github.com/gin-gonic/gin"
	"github.com/go-sphere/httpx"
)

func adaptMiddleware(middleware httpx.Middleware) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		fc := &ginContext{
			ctx: ctx,
		}
		middleware(fc)
	}
}

func adaptMiddlewares(middlewares []httpx.Middleware) []gin.HandlerFunc {
	if len(middlewares) == 0 {
		return nil
	}
	gMid := make([]gin.HandlerFunc, len(middlewares))
	for i, m := range middlewares {
		gMid[i] = adaptMiddleware(m)
	}
	return gMid
}

func AdaptGinMiddleware(middleware gin.HandlerFunc) httpx.Middleware {
	return func(ctx httpx.Context) {
		fc, ok := ctx.(*ginContext)
		if !ok {
			panic("AdaptGinMiddleware: gin context type error")
		}
		middleware(fc.ctx)
	}
}
