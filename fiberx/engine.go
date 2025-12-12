package fiberx

import (
	"net/http"

	"github.com/go-sphere/httpx"
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/adaptor"
)

var _ httpx.Engine = (*Engine)(nil)

type Option = httpx.Option[*fiber.App]
type Engine struct {
	engine       *fiber.App
	middleware   *httpx.MiddlewareChain
	errorHandler httpx.ErrorHandler
}

func New(opts ...Option) httpx.Engine {
	conf := httpx.NewConfig(opts...)
	if conf.Engine == nil {
		conf.Engine = fiber.New()
	}
	middleware := httpx.NewMiddlewareChain()
	middleware.Use(conf.Middleware.Middlewares()...)

	return &Engine{
		engine:       conf.Engine,
		middleware:   middleware,
		errorHandler: conf.ErrorHandler,
	}
}

func (e *Engine) Use(middleware ...httpx.Middleware) {
	e.middleware.Use(middleware...)
}

func (e *Engine) Group(prefix string, m ...httpx.Middleware) httpx.Router {
	middleware := e.middleware.Clone()
	middleware.Use(m...)
	return &Router{
		group:        e.engine.Group(prefix),
		middleware:   middleware,
		errorHandler: e.errorHandler,
	}
}

func (e *Engine) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	adaptor.FiberApp(e.engine).ServeHTTP(w, req)
}
