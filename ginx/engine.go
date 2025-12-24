package ginx

import (
	"context"
	"net/http"
	"sync/atomic"

	"github.com/gin-gonic/gin"
	"github.com/go-sphere/httpx"
)

var _ httpx.Engine = (*Engine)(nil)

type ErrorHandler func(ctx *gin.Context, err error)

type Config struct {
	engine     *gin.Engine
	server     *http.Server
	errHandler ErrorHandler
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
	if conf.server == nil {
		conf.server = &http.Server{
			Addr: ":8080",
		}
	}
	if conf.errHandler == nil {
		conf.errHandler = func(ctx *gin.Context, err error) {
			ctx.JSON(500, gin.H{
				"error": err.Error(),
			})
			ctx.Abort()
		}
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

func WithErrorHandler(errHandler ErrorHandler) Option {
	return func(conf *Config) {
		conf.errHandler = errHandler
	}
}

type Engine struct {
	engine     *gin.Engine
	server     *http.Server
	errHandler ErrorHandler
	running    atomic.Bool
}

// New constructs a gin-backed Engine using core options.
func New(opts ...Option) httpx.Engine {
	conf := NewConfig(opts...)
	conf.server.Handler = conf.engine
	return &Engine{
		engine:     conf.engine,
		server:     conf.server,
		errHandler: conf.errHandler,
	}
}

func (e *Engine) Use(middleware ...httpx.Middleware) {
	e.engine.Use(adaptMiddlewares(middleware, e.errHandler)...)
}

func (e *Engine) Group(prefix string, m ...httpx.Middleware) httpx.Router {
	return &Router{
		group:      e.engine.Group(prefix, adaptMiddlewares(m, e.errHandler)...),
		errHandler: e.errHandler,
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
