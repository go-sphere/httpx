package fiberx

import (
	"io/fs"
	"path"
	"strings"

	"github.com/go-sphere/httpx"
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/static"
)

var _ httpx.Router = (*Router)(nil)

type Router struct {
	basePath     string
	group        fiber.Router
	errorHandler httpx.ErrorHandler
}

func (r *Router) Use(m ...httpx.Middleware) {
	r.group.Use(toMiddlewares(m, r.errorHandler)...)
}

func (r *Router) BasePath() string {
	return r.basePath
}

func (r *Router) Group(prefix string, m ...httpx.Middleware) httpx.Router {
	return &Router{
		basePath:     joinPaths(r.basePath, prefix),
		group:        r.group.Group(prefix, toMiddlewares(m, r.errorHandler)...),
		errorHandler: r.errorHandler,
	}
}

func (r *Router) Handle(method, path string, h httpx.Handler) {
	methods := []string{strings.ToUpper(method)}
	r.group.Add(methods, path, r.toFiberHandler(h))
}

func (r *Router) Any(path string, h httpx.Handler) {
	r.group.All(path, r.toFiberHandler(h))
}

func (r *Router) Static(prefix, root string) {
	r.group.Use(prefix, static.New(root))
}

func (r *Router) StaticFS(prefix string, fs fs.FS) {
	r.group.Use(prefix, static.Config{FS: fs})
}

func (r *Router) toFiberHandler(h httpx.Handler) fiber.Handler {
	return func(fc fiber.Ctx) error {
		ctx := newFiberContext(fc, r.errorHandler)
		if err := h(ctx); err != nil {
			(r.errorHandler)(ctx, err)
		}
		return nil
	}
}

func joinPaths(absolutePath, relativePath string) string {
	if relativePath == "" {
		return absolutePath
	}
	finalPath := path.Join(absolutePath, relativePath)
	appendSlash := lastChar(relativePath) == '/' && lastChar(finalPath) != '/'
	if appendSlash {
		return finalPath + "/"
	}
	return finalPath
}

func lastChar(str string) uint8 {
	if str == "" {
		panic("The length of the string can't be 0")
	}
	return str[len(str)-1]
}
