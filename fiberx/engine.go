package fiberx

import (
	"context"
	"net"

	"github.com/go-sphere/httpx"
	"github.com/gofiber/fiber/v3"
)

var _ httpx.Engine = (*Engine)(nil)

type Config struct {
	engine *fiber.App
	listen func(*fiber.App) error
}

type Option func(*Config)

func NewConfig(opts ...Option) *Config {
	conf := Config{}
	for _, opt := range opts {
		opt(&conf)
	}
	if conf.engine == nil {
		conf.engine = fiber.New()
	}
	if conf.listen == nil {
		conf.listen = func(app *fiber.App) error {
			return app.Listen(":8080")
		}
	}
	return &conf
}

func WithEngine(engine *fiber.App) Option {
	return func(conf *Config) {
		conf.engine = engine
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
	engine      *fiber.App
	middlewares []httpx.Middleware
	listen      func(*fiber.App) error
}

func New(opts ...Option) httpx.Engine {
	conf := NewConfig(opts...)
	return &Engine{
		engine:      conf.engine,
		middlewares: []httpx.Middleware{},
		listen:      conf.listen,
	}
}

func (e *Engine) Use(middlewares ...httpx.Middleware) {
	e.middlewares = append(e.middlewares, middlewares...)
}

func (e *Engine) Group(prefix string, m ...httpx.Middleware) httpx.Router {
	return &Router{
		basePath:    joinPaths("/", prefix),
		group:       e.engine.Group(prefix),
		middlewares: cloneMiddlewares(e.middlewares, m...),
	}
}

func (e *Engine) Start() error {
	return e.listen(e.engine)
}

func (e *Engine) Stop(ctx context.Context) error {
	return e.engine.ShutdownWithContext(ctx)
}
