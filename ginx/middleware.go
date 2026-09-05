package ginx

import (
	"errors"

	"github.com/gin-gonic/gin"
	"github.com/go-sphere/httpx"
)

func adaptMiddleware(middleware httpx.Middleware, errHandler ErrorHandler) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		fc := newGinContext(ctx)
		if err := middleware(fc); err != nil {
			_ = ctx.Error(err)
			if !ctx.IsAborted() {
				errHandler(ctx, err)
			}
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

// AdaptGinMiddleware wraps a native gin middleware as httpx.Middleware.
// The error snapshot is taken before the native middleware runs so errors it
// records (directly or via the chain it drives with c.Next()) are returned to
// the wrapper, and the downstream chain is only driven when the native
// middleware did not abort.
func AdaptGinMiddleware(middleware gin.HandlerFunc) httpx.Middleware {
	return func(ctx httpx.Context) error {
		gc, ok := httpx.AsNativeContext[*gin.Context](ctx)
		if !ok {
			return errors.New("AdaptGinMiddleware: gin context type error")
		}
		before := len(gc.Errors)
		middleware(gc)
		if !gc.IsAborted() {
			// Errors from downstream are recorded in gc.Errors and collected below.
			_ = ctx.Next()
		}
		return collectGinErrors(gc, before)
	}
}

func collectGinErrors(gc *gin.Context, from int) error {
	if len(gc.Errors) <= from {
		return nil
	}
	errList := make([]error, 0, len(gc.Errors)-from)
	for _, e := range gc.Errors[from:] {
		if e != nil {
			errList = append(errList, e.Err)
		}
	}
	return joinErrors(errList)
}
