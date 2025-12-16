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
	middlewares  []httpx.Middleware
	errorHandler httpx.ErrorHandler
}

func (r *Router) Use(m ...httpx.Middleware) {
	r.middlewares = append(r.middlewares, m...)
}

func (r *Router) BasePath() string {
	return r.basePath
}

func (r *Router) Group(prefix string, m ...httpx.Middleware) httpx.Router {
	return &Router{
		basePath:     joinPaths(r.basePath, prefix),
		group:        r.group.Group(prefix),
		middlewares:  cloneMiddlewares(r.middlewares, m...),
		errorHandler: r.errorHandler,
	}
}

func (r *Router) Handle(method, path string, h httpx.Handler) {
	methods := []string{strings.ToUpper(method)}
	handler, handlers := r.adaptHandler(h)
	r.group.Add(methods, path, handler, handlers...)
}

func (r *Router) Any(path string, h httpx.Handler) {
	handler, handlers := r.adaptHandler(h)
	r.group.All(path, handler, handlers...)
}

func (r *Router) Static(prefix, root string) {
	handlers := []any{prefix}
	handlers = append(handlers, r.combineHandlers(static.New(root)))
	r.group.Use(handlers...)
}

func (r *Router) StaticFS(prefix string, fs fs.FS) {
	handlers := []any{prefix}
	handlers = append(handlers, r.combineHandlers(static.New("", static.Config{FS: fs})))
	r.group.Use(handlers...)
}

func (r *Router) combineHandlers(h fiber.Handler) []any {
	return append(adaptMiddlewares(r.middlewares, r.errorHandler), h)
}

func (r *Router) adaptHandler(h httpx.Handler) (any, []any) {
	handlers := r.combineHandlers(func(fc fiber.Ctx) error {
		ctx := newFiberContext(fc, r.errorHandler)
		if err := h(ctx); err != nil {
			r.errorHandler(ctx, err)
		}
		return nil
	})
	if len(handlers) == 0 {
		return nil, nil
	}
	if len(handlers) == 1 {
		return handlers[0], nil
	}
	return handlers[0], handlers[1:]
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
