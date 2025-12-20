package ginx

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"

	"github.com/gin-gonic/gin"
	"github.com/go-sphere/httpx"
)

var _ httpx.Engine = (*Engine)(nil)

type Config struct {
	engine *gin.Engine
	server *http.Server
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
	engine  *gin.Engine
	server  *http.Server
	running atomic.Bool
}

// New constructs a gin-backed Engine using core options.
func New(opts ...Option) httpx.Engine {
	conf := NewConfig(opts...)
	conf.server.Handler = conf.engine
	return &Engine{
		engine: conf.engine,
		server: conf.server,
	}
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

	// Start serving in a goroutine so Start() doesn't block
	go func() {
		err := e.server.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			e.running.Store(false)
		}
	}()

	return nil
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
