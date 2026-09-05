package ginx

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"

	"github.com/gin-gonic/gin"
	"github.com/go-sphere/httpx"
)

var _ httpx.Engine = (*Engine)(nil)

type ErrorHandler func(ctx *gin.Context, err error)

type Config struct {
	engine            *gin.Engine
	server            *http.Server
	errHandler        ErrorHandler
	defaultMiddleware bool
}

type Option func(*Config)

func NewConfig(opts ...Option) *Config {
	conf := Config{}
	for _, opt := range opts {
		opt(&conf)
	}
	if conf.engine == nil {
		conf.engine = gin.New()
	}
	if conf.server == nil {
		conf.server = &http.Server{
			Addr: ":8080",
		}
	}
	if conf.errHandler == nil {
		conf.errHandler = DefaultErrorHandler
	}
	return &conf
}

// DefaultErrorHandler is the native error handler installed when no custom
// handler is configured. It renders the standard httpx error body.
func DefaultErrorHandler(ctx *gin.Context, err error) {
	status, body := httpx.RenderError(err)
	ctx.JSON(status, body)
	ctx.Abort()
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

// WithHTTPXErrorHandler installs a framework-neutral error handler. The
// handler receives a real httpx.Context backed by the gin context, so the
// same error-rendering code can be shared across all adapters.
func WithHTTPXErrorHandler(errHandler httpx.ErrorHandler) Option {
	return func(conf *Config) {
		if errHandler == nil {
			return
		}
		conf.errHandler = func(ctx *gin.Context, err error) {
			errHandler(FromGin(ctx), err)
			if !ctx.IsAborted() {
				ctx.Abort()
			}
		}
	}
}

// WithAddr sets the listen address. It is the framework-neutral equivalent of
// WithServerAddr, present on every adapter.
func WithAddr(addr string) Option {
	return WithServerAddr(addr)
}

// WithDefaultMiddleware enables Gin's default Logger and Recovery middleware.
// Without this option the engine starts with no middleware, matching the other adapters.
func WithDefaultMiddleware() Option {
	return func(conf *Config) {
		conf.defaultMiddleware = true
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
	if conf.defaultMiddleware {
		conf.engine.Use(gin.Logger(), gin.Recovery())
	}
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
	return httpx.Start(e.server)
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

// Do serves req in-process through the gin engine and returns the buffered
// response. It implements httpx.TestRequester.
func (e *Engine) Do(req *http.Request) (*http.Response, error) {
	rr := httptest.NewRecorder()
	e.engine.ServeHTTP(rr, req)
	return rr.Result(), nil
}
