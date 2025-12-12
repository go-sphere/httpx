package ginx

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/go-sphere/httpx"
)

var _ httpx.Router = (*router)(nil)

type router struct {
	engine       *gin.Engine
	group        *gin.RouterGroup
	middleware   *httpx.MiddlewareChain
	errorHandler httpx.ErrorHandler
}

func (r *router) Use(m ...httpx.Middleware) {
	r.middleware.Use(m...)
}

func (r *router) Group(prefix string, m ...httpx.Middleware) httpx.Router {
	child := &router{
		engine:       r.engine,
		group:        r.group.Group(prefix),
		middleware:   r.middleware.Clone(),
		errorHandler: r.errorHandler,
	}
	child.Use(m...)
	return child
}

func (r *router) Handle(method, path string, h httpx.Handler) {
	r.group.Handle(method, path, r.toGinHandler(h))
}

func (r *router) Any(path string, h httpx.Handler) {
	r.group.Any(path, r.toGinHandler(h))
}

func (r *router) Static(prefix, root string) {
	r.group.Static(prefix, root)
}

func (r *router) Mount(path string, h http.Handler) {
	base := strings.TrimSuffix(path, "/")
	if base == "" {
		base = "/"
	}
	wrapped := func(ctx httpx.Context) error {
		if gc, ok := ctx.(*ginContext); ok {
			h.ServeHTTP(gc.ctx.Writer, gc.ctx.Request)
		}
		return nil
	}
	r.group.Any(base, r.toGinHandler(wrapped))
	if base != "/" {
		r.group.Any(base+"/*path", r.toGinHandler(wrapped))
	} else {
		r.group.Any("/*path", r.toGinHandler(wrapped))
	}
}

func (r *router) toGinHandler(h httpx.Handler) gin.HandlerFunc {
	handler := r.middleware.Then(h)
	return func(gc *gin.Context) {
		ctx := newGinContext(gc, r.errorHandler)
		if err := handler(ctx); err != nil {
			(r.errorHandler)(ctx, err)
		}
	}
}
