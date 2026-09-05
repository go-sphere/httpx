package hertzx

import (
	"context"
	"errors"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/go-sphere/httpx"
)

func adaptMiddleware(middleware httpx.Middleware, errHandler ErrorHandler) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		fc := newHertzContext(c, ctx)
		if err := middleware(fc); err != nil {
			_ = ctx.Error(err)
			if !ctx.IsAborted() && !hertzResponseCommitted(ctx) {
				errHandler(c, ctx, err)
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

// AdaptHertzMiddleware wraps a native hertz middleware as httpx.Middleware.
// The error snapshot is taken before the native middleware runs so errors it
// records (directly or via the chain it drives with ctx.Next()) are returned
// to the wrapper, and the downstream chain is only driven when the native
// middleware did not abort.
func AdaptHertzMiddleware(middleware app.HandlerFunc) httpx.Middleware {
	return func(ctx httpx.Context) error {
		rc, ok := httpx.AsNativeContext[*app.RequestContext](ctx)
		if !ok {
			return errors.New("AdaptHertzMiddleware: invalid context type")
		}
		before := len(rc.Errors)
		middleware(ctx.Context(), rc)
		if !rc.IsAborted() {
			// Errors from downstream are recorded in rc.Errors and collected below.
			_ = ctx.Next()
		}
		return collectHertzErrors(rc, before)
	}
}

func collectHertzErrors(rc *app.RequestContext, from int) error {
	if len(rc.Errors) <= from {
		return nil
	}
	errList := make([]error, 0, len(rc.Errors)-from)
	for _, e := range rc.Errors[from:] {
		if e != nil {
			errList = append(errList, e.Err)
		}
	}
	return joinErrors(errList)
}
