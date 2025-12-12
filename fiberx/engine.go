package fiberx

import (
	"net/http"

	"github.com/go-sphere/httpx"
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/adaptor"
)

var _ httpx.Engine = (*engine)(nil)

type engine struct {
	*router
}

func New(opts ...httpx.Option[*fiber.App]) httpx.Engine {
	conf := httpx.NewConfig(opts...)
	if conf.Engine == nil {
		conf.Engine = fiber.New()
	}
	middleware := httpx.NewMiddlewareChain()
	middleware.Use(conf.Middleware.Middlewares()...)

	errorHandler := conf.ErrorHandler

	return &engine{
		router: &router{
			app:          conf.Engine,
			group:        conf.Engine.Group("/"),
			middleware:   middleware,
			errorHandler: errorHandler,
		},
	}
}

func (e *engine) RegisterErrorHandler(h httpx.ErrorHandler) {
	e.errorHandler = resolveErrorHandler(h)
}

func (e *engine) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	adaptor.FiberApp(e.app).ServeHTTP(w, req)
}

func resolveErrorHandler(h httpx.ErrorHandler) httpx.ErrorHandler {
	if h != nil {
		return h
	}
	return func(ctx httpx.Context, err error) {
		if err == nil {
			return
		}
		if !ctx.IsAborted() {
			_ = ctx.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
	}
}
