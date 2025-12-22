package echox

import (
	"context"
	"net/http"
	"sync/atomic"

	"github.com/go-sphere/httpx"
	"github.com/labstack/echo/v4"
)

var _ httpx.Engine = (*Engine)(nil)

type Config struct {
	engine *echo.Echo
	server *http.Server
}

type Option func(*Config)

func NewConfig(opts ...Option) *Config {
	conf := &Config{}
	for _, opt := range opts {
		opt(conf)
	}
	if conf.engine == nil {
		conf.engine = echo.New()
	}
	if conf.server == nil {
		conf.server = &http.Server{
			Addr: ":8080",
		}
	}
	return conf
}

func WithEngine(engine *echo.Echo) Option {
	return func(conf *Config) {
		conf.engine = engine
	}
}

func WithServer(server *http.Server) Option {
	return func(conf *Config) {
		conf.server = server
	}
}

func WithServerAddr(addr string) Option {
	return func(conf *Config) {
		if conf.server == nil {
			conf.server = &http.Server{
				Addr: addr,
			}
		} else {
			conf.server.Addr = addr
		}
	}
}

type Engine struct {
	engine  *echo.Echo
	server  *http.Server
	running atomic.Bool
}

func New(opts ...Option) httpx.Engine {
	conf := NewConfig(opts...)
	conf.server.Handler = conf.engine
	engine := &Engine{
		engine: conf.engine,
		server: conf.server,
	}
	engine.running.Store(false)
	return engine
}

func (e *Engine) Use(middleware ...httpx.Middleware) {
	e.engine.Use(adaptMiddlewares(middleware)...)
}

func (e *Engine) Group(prefix string, m ...httpx.Middleware) httpx.Router {
	return &Router{
		group:    e.engine.Group(prefix, adaptMiddlewares(m)...),
		basePath: joinPaths("/", prefix),
	}
}

func (e *Engine) Start() error {
	e.running.Store(true)
	defer e.running.Store(false)
	return e.server.ListenAndServe()
}

func (e *Engine) Stop(ctx context.Context) error {
	err := httpx.Close(ctx, e.server)
	if err == nil {
		e.running.Store(false)
	}
	return err
}

// IsRunning returns true if the server is currently running.
func (e *Engine) IsRunning() bool {
	return e.running.Load()
}
