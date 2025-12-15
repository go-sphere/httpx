package ginx

import (
	"errors"
	"io/fs"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/go-sphere/httpx"
)

var _ httpx.Router = (*Router)(nil)

type Router struct {
	group        *gin.RouterGroup
	middleware   *httpx.MiddlewareChain
	errorHandler httpx.ErrorHandler
}

func (r *Router) Use(m ...httpx.Middleware) {
	r.middleware.Use(m...)
}

func (r *Router) BasePath() string {
	return r.group.BasePath()
}

func (r *Router) Group(prefix string, m ...httpx.Middleware) httpx.Router {
	child := &Router{
		group:        r.group.Group(prefix),
		middleware:   r.middleware.Clone(),
		errorHandler: r.errorHandler,
	}
	child.Use(m...)
	return child
}

func (r *Router) Handle(method, path string, h httpx.Handler) {
	r.group.Handle(method, path, r.toGinHandler(h))
}

func (r *Router) Any(path string, h httpx.Handler) {
	r.group.Any(path, r.toGinHandler(h))
}

func (r *Router) Static(prefix, root string) {
	r.group.Static(prefix, root)
}

func (r *Router) StaticFS(prefix string, fs fs.FS) {
	r.group.StaticFS(prefix, http.FS(fs))
}

func (r *Router) toGinHandler(h httpx.Handler) gin.HandlerFunc {
	handler := r.middleware.Then(h)
	return func(gc *gin.Context) {
		ctx := newGinContext(gc, r.errorHandler)
		if err := handler(ctx); err != nil {
			r.errorHandler(ctx, err)
		}
	}
}

func ToMiddleware(middleware gin.HandlerFunc, order httpx.MiddlewareOrder) httpx.Middleware {
	return func(next httpx.Handler) httpx.Handler {
		return func(ctx httpx.Context) error {
			gc, ok := ctx.(*ginContext)
			if !ok {
				return errors.New("ginContext required")
			}
			switch order {
			case httpx.MiddlewareAfterNext:
				err := next(ctx)
				if err != nil {
					return err
				}
				if gc.ctx.IsAborted() {
					return nil
				}
				middleware(gc.ctx)
				return nil
			default:
				middleware(gc.ctx)
				if gc.ctx.IsAborted() {
					return nil
				}
				return next(ctx)
			}
		}
	}
}
