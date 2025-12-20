package fiberx

import (
	"context"
	"net"
	"sync/atomic"

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
	running     atomic.Bool
}

func New(opts ...Option) httpx.Engine {
	conf := NewConfig(opts...)
	engine := &Engine{
		engine:      conf.engine,
		middlewares: []httpx.Middleware{},
		listen:      conf.listen,
	}
	engine.running.Store(false)
	return engine
}

func (e *Engine) Use(middlewares ...httpx.Middleware) {
	e.middlewares = append(e.middlewares, middlewares...)
	// Register middlewares globally on the fiber app
	for _, middleware := range middlewares {
		e.engine.Use(adaptMiddleware(middleware))
	}
}

func (e *Engine) Group(prefix string, m ...httpx.Middleware) httpx.Router {
	return &Router{
		basePath:    joinPaths("/", prefix),
		group:       e.engine.Group(prefix),
		middlewares: cloneMiddlewares([]httpx.Middleware{}, m...), // Don't include global middlewares here since they're already registered
	}
}

func (e *Engine) Start() error {
	e.running.Store(true)
	err := e.listen(e.engine)
	if err != nil {
		e.running.Store(false)
	}
	return err
}

func (e *Engine) Stop(ctx context.Context) error {
	err := e.engine.ShutdownWithContext(ctx)
	if err == nil {
		e.running.Store(false)
	}
	return err
}

// IsRunning returns true if the server is currently running.
func (e *Engine) IsRunning() bool {
	return e.running.Load()
}
