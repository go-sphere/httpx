package hertzx

import (
	"context"
	"sync/atomic"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/go-sphere/httpx"
)

var _ httpx.Engine = (*Engine)(nil)

type Config struct {
	engine *server.Hertz
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
	return &conf
}
func WithEngine(engine *server.Hertz) Option {
	return func(conf *Config) {
		conf.engine = engine
	}
}

type Engine struct {
	engine  *server.Hertz
	running atomic.Bool
}

func New(opts ...Option) httpx.Engine {
	conf := NewConfig(opts...)
	engine := &Engine{
		engine: conf.engine,
	}
	engine.running.Store(false)
	return engine
}

func (e *Engine) Use(middleware ...httpx.Middleware) {
	e.engine.Use(adaptMiddlewares(middleware)...)
}

func (e *Engine) Group(prefix string, m ...httpx.Middleware) httpx.Router {
	return &Router{
		group: e.engine.Group(prefix, adaptMiddlewares(m)...),
	}
}

func (e *Engine) Start() error {
	e.running.Store(true)
	defer e.running.Store(false)
	return e.engine.Run()
}

func (e *Engine) Stop(ctx context.Context) error {
	err := e.engine.Shutdown(ctx)
	if err == nil {
		e.running.Store(false)
	}
	return err
}

// IsRunning returns true if the server is currently running.
func (e *Engine) IsRunning() bool {
	return e.running.Load()
}
