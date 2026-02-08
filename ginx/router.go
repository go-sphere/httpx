package ginx

import (
	"io/fs"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/go-sphere/httpx"
)

var _ httpx.Router = (*Router)(nil)

type Router struct {
	group      *gin.RouterGroup
	errHandler ErrorHandler
}

func (r *Router) Use(m ...httpx.Middleware) {
	r.group.Use(adaptMiddlewares(m, r.errHandler)...)
}

func (r *Router) BasePath() string {
	return r.group.BasePath()
}

func (r *Router) Group(prefix string, m ...httpx.Middleware) httpx.Router {
	return &Router{
		group:      r.group.Group(prefix, adaptMiddlewares(m, r.errHandler)...),
		errHandler: r.errHandler,
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

// GET registers a new GET route for a path with matching handler.
func (r *Router) GET(path string, h httpx.Handler) {
	r.group.GET(path, r.toGinHandler(h))
}

// POST registers a new POST route for a path with matching handler.
func (r *Router) POST(path string, h httpx.Handler) {
	r.group.POST(path, r.toGinHandler(h))
}

// PUT registers a new PUT route for a path with matching handler.
func (r *Router) PUT(path string, h httpx.Handler) {
	r.group.PUT(path, r.toGinHandler(h))
}

// DELETE registers a new DELETE route for a path with matching handler.
func (r *Router) DELETE(path string, h httpx.Handler) {
	r.group.DELETE(path, r.toGinHandler(h))
}

// PATCH registers a new PATCH route for a path with matching handler.
func (r *Router) PATCH(path string, h httpx.Handler) {
	r.group.PATCH(path, r.toGinHandler(h))
}

// HEAD registers a new HEAD route for a path with matching handler.
func (r *Router) HEAD(path string, h httpx.Handler) {
	r.group.HEAD(path, r.toGinHandler(h))
}

// OPTIONS registers a new OPTIONS route for a path with matching handler.
func (r *Router) OPTIONS(path string, h httpx.Handler) {
	r.group.OPTIONS(path, r.toGinHandler(h))
}

func (r *Router) toGinHandler(h httpx.Handler) gin.HandlerFunc {
	return func(gc *gin.Context) {
		ctx := newGinContext(gc)
		if err := h(ctx); err != nil {
			_ = gc.Error(err)
			r.errHandler(gc, err)
			if !gc.IsAborted() {
				gc.Abort()
			}
		}
	}
}
