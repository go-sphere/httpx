package ginx

import (
	"errors"

	"github.com/gin-gonic/gin"
	"github.com/go-sphere/httpx"
)

// Each invocation needs its own Next flag, including when native middleware
// re-enters the chain or an outer handler retains an inner Context.
type ginMiddlewareContext struct {
	ginContext
	nextCalled bool
}

func (c *ginMiddlewareContext) Next() error {
	// Drive Gin directly: forwarding through ginContext.Next adds another
	// call frame per middleware, which is measurable on deep chains.
	c.nextCalled = true
	before := len(c.ctx.Errors)
	c.ctx.Next()
	if len(c.ctx.Errors) <= before {
		return nil
	}
	return collectGinErrors(c.ctx, before)
}

func adaptMiddleware(middleware httpx.Middleware, errHandler ErrorHandler) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		fc := &ginMiddlewareContext{ginContext: newGinContext(ctx)}
		if err := middleware(fc); err != nil {
			_ = ctx.Error(err)
			// Skip the error handler when the response is already committed
			// (or the chain aborted) so a partial response is not corrupted
			// by a second body, matching toGinHandler.
			if !ctx.IsAborted() && !ctx.Writer.Written() {
				errHandler(ctx, err)
				commitErrorStatus(ctx, err)
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
//
// Consider Router.UseNative instead: it registers the gin handler with no
// adapter in between.
func AdaptGinMiddleware(middleware gin.HandlerFunc) httpx.Middleware {
	return ginBridge{middleware: middleware}.run
}

// ginBridge runs a native gin middleware and, unless it aborted, continues the
// httpx chain. The native middleware may also continue the chain itself with
// gin's Next, which works because every httpx middleware occupies its own gin
// handler slot.
type ginBridge struct {
	middleware gin.HandlerFunc
}

func (b ginBridge) run(ctx httpx.Context) error {
	gc, ok := httpx.AsNativeContext[*gin.Context](ctx)
	if !ok {
		return errors.New("AdaptGinMiddleware: gin context type error")
	}
	before := len(gc.Errors)
	b.middleware(gc)
	if !gc.IsAborted() {
		// Errors from downstream are recorded in gc.Errors and collected below.
		_ = ctx.Next()
	}
	return collectGinErrors(gc, before)
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
