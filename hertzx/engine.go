package hertzx

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/go-sphere/httpx"
)

var _ httpx.Engine = (*Engine)(nil)

type Config struct {
	engine       *server.Hertz
	errorHandler httpx.ErrorHandler
}

type Option func(*Config)

func NewConfig(opts ...Option) *Config {
	conf := Config{}
	for _, opt := range opts {
		opt(&conf)
	}
	if conf.engine == nil {
		conf.engine = server.Default()
	}
	if conf.errorHandler == nil {
		conf.errorHandler = httpx.DefaultErrorHandler
	}

	return &conf
}
func WithEngine(engine *server.Hertz) Option {
	return func(conf *Config) {
		conf.engine = engine
	}
}

func WithErrorHandler(handler httpx.ErrorHandler) Option {
	return func(conf *Config) {
		conf.errorHandler = handler
	}
}

type Engine struct {
	engine       *server.Hertz
	middleware   *httpx.MiddlewareChain
	errorHandler httpx.ErrorHandler
}

func New(opts ...Option) *Engine {
	conf := NewConfig(opts...)
	return &Engine{
		engine:       conf.engine,
		middleware:   httpx.NewMiddlewareChain(),
		errorHandler: conf.errorHandler,
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
