package fiberx

import (
	"net/http"
	"strings"

	"github.com/go-sphere/httpx"
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/adaptor"
	"github.com/gofiber/fiber/v3/middleware/static"
)

var _ httpx.Router = (*router)(nil)

type router struct {
	app          *fiber.App
	group        fiber.Router
	middleware   *httpx.MiddlewareChain
	errorHandler httpx.ErrorHandler
}

func (r *router) Use(m ...httpx.Middleware) {
	r.middleware.Use(m...)
}

func (r *router) Group(prefix string, m ...httpx.Middleware) httpx.Router {
	child := &router{
		app:          r.app,
		group:        r.group.Group(prefix),
		middleware:   r.middleware.Clone(),
		errorHandler: r.errorHandler,
	}
	child.Use(m...)
	return child
}

func (r *router) Handle(method, path string, h httpx.Handler) {
	methods := []string{strings.ToUpper(method)}
	r.group.Add(methods, path, r.toFiberHandler(h))
}

func (r *router) Any(path string, h httpx.Handler) {
	r.group.All(path, r.toFiberHandler(h))
}

func (r *router) Static(prefix, root string) {
	r.group.Use(prefix, static.New(root))
}

func (r *router) Mount(path string, h http.Handler) {
	base := strings.TrimSuffix(path, "/")
	if base == "" {
		base = "/"
	}

	fiberHandler := adaptor.HTTPHandler(h)
	wrapped := func(ctx httpx.Context) error {
		if fc, ok := ctx.(*fiberContext); ok {
			return fiberHandler(fc.ctx)
		}
		return nil
	}
	handler := r.toFiberHandler(wrapped)

	r.group.All(base, handler)
	if base == "/" {
		r.group.All("/*", handler)
	} else {
		r.group.All(base+"/*", handler)
	}
}

func (r *router) toFiberHandler(h httpx.Handler) fiber.Handler {
	handler := r.middleware.Then(h)
	return func(fc fiber.Ctx) error {
		ctx := newFiberContext(fc, r.errorHandler)
		if err := handler(ctx); err != nil {
			(r.errorHandler)(ctx, err)
		}
		return nil
	}
}
