package hertzx

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/go-sphere/sphere/server/httpx"
)

var _ httpx.Engine = (*Engine)(nil)

type Engine struct {
	engine       *server.Hertz
	middleware   *httpx.MiddlewareChain
	errorHandler httpx.ErrorHandler
}

func New(opts ...httpx.Option[*server.Hertz]) *Engine {
	conf := httpx.NewConfig(opts...)
	if conf.Engine == nil {
		conf.Engine = server.Default()
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

func (e *Engine) Start() error {
	return e.engine.Run()
}

func (e *Engine) Stop(ctx context.Context) error {
	return e.engine.Shutdown(ctx)
}
