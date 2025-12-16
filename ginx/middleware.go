package ginx

import (
	"fmt"

	"github.com/gin-gonic/gin"
	"github.com/go-sphere/httpx"
)

func toMiddleware(middleware httpx.Middleware, errorHandler httpx.ErrorHandler) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		fc := &ginContext{
			ctx:          ctx,
			errorHandler: errorHandler,
		}
		err := middleware(fc)
		if err != nil {
			errorHandler(fc, err)
		}
	}
}

func toMiddlewares(middlewares []httpx.Middleware, errorHandler httpx.ErrorHandler) []gin.HandlerFunc {
	gMid := make([]gin.HandlerFunc, len(middlewares))
	for i, m := range middlewares {
		gMid[i] = toMiddleware(m, errorHandler)
	}
	return gMid
}

func MiddlewareAdapter(middleware gin.HandlerFunc) httpx.Middleware {
	return func(ctx httpx.Context) error {
		fc, ok := ctx.(*ginContext)
		if !ok {
			return fmt.Errorf("invalid context type: %T", ctx)
		}
		middleware(fc.ctx)
		return nil
	}
}
