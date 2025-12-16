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
	basePath    string
	group       fiber.Router
	middlewares []httpx.Middleware
}

func (r *Router) Use(m ...httpx.Middleware) {
	r.middlewares = append(r.middlewares, m...)
}

func (r *Router) BasePath() string {
	return r.basePath
}

func (r *Router) Group(prefix string, m ...httpx.Middleware) httpx.Router {
	return &Router{
		basePath:    joinPaths(r.basePath, prefix),
		group:       r.group.Group(prefix),
		middlewares: cloneMiddlewares(r.middlewares, m...),
	}
}

func (r *Router) Handle(method, path string, h httpx.Handler) {
	methods := []string{strings.ToUpper(method)}
	handler, handlers := splitHandlers(r.adaptHandler(h))
	r.group.Add(methods, path, handler, handlers...)
}

func (r *Router) Any(path string, h httpx.Handler) {
	handler, handlers := splitHandlers(r.adaptHandler(h))
	r.group.All(path, handler, handlers...)
}

func (r *Router) Static(prefix, root string) {
	r.group.Use(append([]any{prefix}, r.combineHandlers(static.New(root))...)...)
}

func (r *Router) StaticFS(prefix string, fs fs.FS) {
	r.group.Use(append([]any{prefix}, r.combineHandlers(static.New("", static.Config{FS: fs}))...))
}

func (r *Router) combineHandlers(h fiber.Handler) []any {
	mid := make([]any, 0, len(r.middlewares)+1)
	for _, m := range r.middlewares {
		mid = append(mid, adaptMiddleware(m))
	}
	mid = append(mid, h)
	return mid
}

func (r *Router) adaptHandler(h httpx.Handler) []any {
	return r.combineHandlers(func(ctx fiber.Ctx) error {
		fc := newFiberContext(ctx)
		h(fc)
		return nil
	})
}

func splitHandlers(handlers []any) (any, []any) {
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
