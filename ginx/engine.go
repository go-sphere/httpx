package ginx

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/go-sphere/httpx"
)

var _ httpx.Engine = (*Engine)(nil)

type Config struct {
	engine       *gin.Engine
	server       *http.Server
	errorHandler httpx.ErrorHandler
}

type Option func(*Config)

func NewConfig(opts ...Option) *Config {
	conf := Config{}
	for _, opt := range opts {
		opt(&conf)
	}
	if conf.engine == nil {
		conf.engine = gin.Default()
	}
	if conf.server != nil {
		conf.server = &http.Server{
			Addr: ":8080",
		}
	}
	if conf.errorHandler == nil {
		conf.errorHandler = httpx.DefaultErrorHandler
	}
	return &conf
}

func WithEngine(engine *gin.Engine) Option {
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
	engine       *gin.Engine
	server       *http.Server
	middleware   *httpx.MiddlewareChain
	errorHandler httpx.ErrorHandler
}

// New constructs a gin-backed Engine using core options.
func New(opts ...Option) httpx.Engine {
	conf := NewConfig(opts...)
	return &Engine{
		engine:       conf.engine,
		server:       conf.server,
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
	e.server.Handler = e.engine
	return httpx.Start(e.server)
}

func (e *Engine) Stop(ctx context.Context) error {
	return httpx.Close(ctx, e.server)
}
