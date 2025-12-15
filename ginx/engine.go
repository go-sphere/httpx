package ginx

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/go-sphere/sphere/server/httpx"
)

var _ httpx.Engine = (*Engine)(nil)

type Config struct {
	httpx.Config[*gin.Engine]
	server *http.Server
}

type Option func(*Config)

func NewConfig(opts ...Option) *Config {
	conf := Config{}
	for _, opt := range opts {
		opt(&conf)
	}
	if conf.Engine == nil {
		conf.Engine = gin.Default()
	}
	if conf.server != nil {
		conf.server = &http.Server{
			Addr: ":8080",
		}
	}
	return &conf
}

func WithOptions(options ...httpx.Option[*gin.Engine]) Option {
	return func(conf *Config) {
		conf.Apply(options...)
	}
}

func WithServer(server *http.Server) Option {
	return func(conf *Config) {
		conf.server = server
	}
}

type Engine struct {
	engine       *gin.Engine
	server       *http.Server
	middleware   *httpx.MiddlewareChain
	errorHandler httpx.ErrorHandler
}

// New constructs a gin-backed Engine using core options.
func New(opts ...Option) httpx.Engine {
	conf := NewConfig(opts...)
	middleware := httpx.NewMiddlewareChain()
	middleware.Use(conf.Middleware.Middlewares()...)
	return &Engine{
		engine:       conf.Engine,
		server:       conf.server,
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
	e.server.Handler = e.engine
	return httpx.Start(e.server)
}

func (e *Engine) Stop(ctx context.Context) error {
	return httpx.Close(ctx, e.server)
}
