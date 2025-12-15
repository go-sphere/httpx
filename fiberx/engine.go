package fiberx

import (
	"context"
	"net"

	"github.com/go-sphere/httpx"
	"github.com/gofiber/fiber/v3"
)

var _ httpx.Engine = (*Engine)(nil)

type Config struct {
	httpx.Config[*fiber.App]
	listen func(*fiber.App) error
}

type Option func(*Config)

func NewConfig(opts ...Option) *Config {
	conf := Config{}
	for _, opt := range opts {
		opt(&conf)
	}
	if conf.Engine == nil {
		conf.Engine = fiber.New()
	}
	if conf.listen != nil {
		conf.listen = func(app *fiber.App) error {
			return app.Listen(":8080")
		}
	}
	return &conf
}

func WithOptions(options ...httpx.Option[*fiber.App]) Option {
	return func(conf *Config) {
		conf.Apply(options...)
	}
}

func WithListen(addr string, config ...fiber.ListenConfig) Option {
	return func(conf *Config) {
		conf.listen = func(app *fiber.App) error {
			return app.Listen(addr, config...)
		}
	}
}

func WithListener(ln net.Listener, config ...fiber.ListenConfig) Option {
	return func(conf *Config) {
		conf.listen = func(app *fiber.App) error {
			return app.Listener(ln, config...)
		}
	}
}

type Engine struct {
	engine       *fiber.App
	middleware   *httpx.MiddlewareChain
	errorHandler httpx.ErrorHandler
	listen       func(*fiber.App) error
}

func New(opts ...Option) httpx.Engine {
	conf := NewConfig(opts...)
	middleware := httpx.NewMiddlewareChain()
	middleware.Use(conf.Middleware.Middlewares()...)
	return &Engine{
		engine:       conf.Engine,
		middleware:   middleware,
		errorHandler: conf.ErrorHandler,
		listen:       conf.listen,
	}
}

func (e *Engine) Use(middleware ...httpx.Middleware) {
	e.middleware.Use(middleware...)
}

func (e *Engine) Group(prefix string, m ...httpx.Middleware) httpx.Router {
	middleware := e.middleware.Clone()
	middleware.Use(m...)
	return &Router{
		basePath:     joinPaths("/", prefix),
		group:        e.engine.Group(prefix),
		middleware:   middleware,
		errorHandler: e.errorHandler,
	}
}

func (e *Engine) Start() error {
	return e.listen(e.engine)
}

func (e *Engine) Stop(ctx context.Context) error {
	return e.engine.ShutdownWithContext(ctx)
}
