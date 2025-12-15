package fiberx

import (
	"errors"
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
	middleware   *httpx.MiddlewareChain
	errorHandler httpx.ErrorHandler
}

func (r *Router) Use(m ...httpx.Middleware) {
	r.middleware.Use(m...)
}

func (r *Router) BasePath() string {
	return r.basePath
}

func (r *Router) Group(prefix string, m ...httpx.Middleware) httpx.Router {
	child := &Router{
		basePath:     joinPaths(r.basePath, prefix),
		group:        r.group.Group(prefix),
		middleware:   r.middleware.Clone(),
		errorHandler: r.errorHandler,
	}
	child.Use(m...)
	return child
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
	handler := r.middleware.Then(h)
	return func(fc fiber.Ctx) error {
		ctx := newFiberContext(fc, r.errorHandler)
		if err := handler(ctx); err != nil {
			(r.errorHandler)(ctx, err)
		}
		return nil
	}
}

func ToMiddleware(middleware fiber.Handler, order httpx.MiddlewareOrder) httpx.Middleware {
	return func(next httpx.Handler) httpx.Handler {
		return func(ctx httpx.Context) error {
			fc, ok := ctx.(*fiberContext)
			if !ok {
				return errors.New("fiberContext required")
			}
			switch order {
			case httpx.MiddlewareAfterNext:
				err := next(ctx)
				if err != nil {
					return err
				}
				if fc.IsAborted() {
					return nil
				}
				return middleware(fc.ctx)
			default:
				err := middleware(fc.ctx)
				if err != nil {
					return err
				}
				if fc.IsAborted() {
					return nil
				}
				return next(ctx)
			}
		}
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
