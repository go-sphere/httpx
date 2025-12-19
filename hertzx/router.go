package hertzx

import (
	"context"
	"io/fs"
	"net/http"
	"path"
	"strings"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/adaptor"
	"github.com/cloudwego/hertz/pkg/route"
	"github.com/go-sphere/httpx"
)

var _ httpx.Router = (*Router)(nil)

type Router struct {
	group *route.RouterGroup
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
	method = strings.ToUpper(method)
	r.group.Handle(method, path, r.toHertzHandler(h))
}

func (r *Router) Any(path string, h httpx.Handler) {
	r.group.Any(path, r.toHertzHandler(h))
}

func (r *Router) Static(prefix, root string) {
	r.group.Static(prefix, root)
}

func (r *Router) StaticFS(prefix string, fs fs.FS) {
	if strings.Contains(prefix, ":") || strings.Contains(prefix, "*") {
		panic("URL parameters can not be used when serving a static folder")
	}
	absolutePath := path.Join(r.group.BasePath(), prefix)
	fileServer := http.StripPrefix(absolutePath, http.FileServer(http.FS(fs)))
	handler := adaptor.HertzHandler(fileServer)
	urlPattern := path.Join(prefix, "/*filepath")
	r.group.GET(urlPattern, handler)
	r.group.HEAD(urlPattern, handler)
}

// GET registers a new GET route for a path with matching handler.
func (r *Router) GET(path string, h httpx.Handler) {
	r.group.GET(path, r.toHertzHandler(h))
}

// POST registers a new POST route for a path with matching handler.
func (r *Router) POST(path string, h httpx.Handler) {
	r.group.POST(path, r.toHertzHandler(h))
}

// PUT registers a new PUT route for a path with matching handler.
func (r *Router) PUT(path string, h httpx.Handler) {
	r.group.PUT(path, r.toHertzHandler(h))
}

// DELETE registers a new DELETE route for a path with matching handler.
func (r *Router) DELETE(path string, h httpx.Handler) {
	r.group.DELETE(path, r.toHertzHandler(h))
}

// PATCH registers a new PATCH route for a path with matching handler.
func (r *Router) PATCH(path string, h httpx.Handler) {
	r.group.PATCH(path, r.toHertzHandler(h))
}

// HEAD registers a new HEAD route for a path with matching handler.
func (r *Router) HEAD(path string, h httpx.Handler) {
	r.group.HEAD(path, r.toHertzHandler(h))
}

// OPTIONS registers a new OPTIONS route for a path with matching handler.
func (r *Router) OPTIONS(path string, h httpx.Handler) {
	r.group.OPTIONS(path, r.toHertzHandler(h))
}

func (r *Router) toHertzHandler(h httpx.Handler) app.HandlerFunc {
	return func(ctx context.Context, rc *app.RequestContext) {
		hc := newHertzContext(ctx, rc)
		h(hc)
	}
}
