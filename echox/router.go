package echox

import (
	"io/fs"
	"path"
	"strings"

	"github.com/go-sphere/httpx"
	"github.com/labstack/echo/v4"
)

var _ httpx.Router = (*Router)(nil)

type Router struct {
	group    *echo.Group
	basePath string
}

func (r *Router) Use(m ...httpx.Middleware) {
	r.group.Use(adaptMiddlewares(m)...)
}

func (r *Router) BasePath() string {
	return r.basePath
}

func (r *Router) Group(prefix string, m ...httpx.Middleware) httpx.Router {
	return &Router{
		group:    r.group.Group(prefix, adaptMiddlewares(m)...),
		basePath: joinPaths(r.basePath, prefix),
	}
}

func (r *Router) Handle(method, path string, h httpx.Handler) {
	r.group.Add(strings.ToUpper(method), path, r.toEchoHandler(h))
}

func (r *Router) Any(path string, h httpx.Handler) {
	r.group.Any(path, r.toEchoHandler(h))
}

func (r *Router) Static(prefix, root string) {
	r.group.Static(prefix, root)
}

func (r *Router) StaticFS(prefix string, filesystem fs.FS) {
	r.group.StaticFS(prefix, filesystem)
}

// GET registers a new GET route for a path with matching handler.
func (r *Router) GET(path string, h httpx.Handler) {
	r.group.GET(path, r.toEchoHandler(h))
}

// POST registers a new POST route for a path with matching handler.
func (r *Router) POST(path string, h httpx.Handler) {
	r.group.POST(path, r.toEchoHandler(h))
}

// PUT registers a new PUT route for a path with matching handler.
func (r *Router) PUT(path string, h httpx.Handler) {
	r.group.PUT(path, r.toEchoHandler(h))
}

// DELETE registers a new DELETE route for a path with matching handler.
func (r *Router) DELETE(path string, h httpx.Handler) {
	r.group.DELETE(path, r.toEchoHandler(h))
}

// PATCH registers a new PATCH route for a path with matching handler.
func (r *Router) PATCH(path string, h httpx.Handler) {
	r.group.PATCH(path, r.toEchoHandler(h))
}

// HEAD registers a new HEAD route for a path with matching handler.
func (r *Router) HEAD(path string, h httpx.Handler) {
	r.group.HEAD(path, r.toEchoHandler(h))
}

// OPTIONS registers a new OPTIONS route for a path with matching handler.
func (r *Router) OPTIONS(path string, h httpx.Handler) {
	r.group.OPTIONS(path, r.toEchoHandler(h))
}

func (r *Router) toEchoHandler(h httpx.Handler) echo.HandlerFunc {
	return func(ec echo.Context) error {
		ctx := newEchoContext(ec)
		h(ctx)
		return ctx.err
	}
}

func joinPaths(absolutePath, relativePath string) string {
	if relativePath == "" {
		return absolutePath
	}
	finalPath := path.Join(absolutePath, relativePath)
	if lastCharIs('/', relativePath) && !lastCharIs('/', finalPath) {
		return finalPath + "/"
	}
	return finalPath
}

func lastCharIs(char uint8, str string) bool {
	if str == "" {
		return false
	}
	return str[len(str)-1] == char
}
