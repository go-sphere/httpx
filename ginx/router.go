package ginx

import (
	"io/fs"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/go-sphere/httpx"
)

var _ httpx.Router = (*Router)(nil)

type Router struct {
	group *gin.RouterGroup
}

func (r *Router) Use(m ...httpx.Middleware) {
	r.group.Use(adaptMiddlewares(m)...)
}

func (r *Router) BasePath() string {
	return r.group.BasePath()
}

func (r *Router) Group(prefix string, m ...httpx.Middleware) httpx.Router {
	return &Router{
		group: r.group.Group(prefix, adaptMiddlewares(m)...),
	}
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
	return func(gc *gin.Context) {
		ctx := newGinContext(gc)
		h(ctx)
	}
}
