package fiberx

import (
	"context"
	"errors"
	"net"
	"net/http"
	"sync/atomic"

	"github.com/go-sphere/httpx"
	"github.com/gofiber/fiber/v3"
)

var _ httpx.Engine = (*Engine)(nil)

type Config struct {
	engine     *fiber.App
	listen     func(*fiber.App) error
	errHandler httpx.ErrorHandler
}

type Option func(*Config)

func NewConfig(opts ...Option) *Config {
	conf := Config{}
	for _, opt := range opts {
		opt(&conf)
	}
	if conf.engine == nil {
		conf.engine = fiber.New(
			fiber.Config{
				ErrorHandler: DefaultErrorHandler,
			},
		)
	}
	if conf.listen == nil {
		conf.listen = func(app *fiber.App) error {
			return app.Listen(":8080")
		}
	}
	return &conf
}

// DefaultErrorHandler renders errors with the standard httpx error body. It
// understands *fiber.Error (framework 404/405/... errors) so their status
// codes are preserved instead of being reported as 500.
//
// fiber.Config is immutable after fiber.New, so when providing your own
// engine via WithEngine, set this (or an equivalent) as fiber.Config's
// ErrorHandler to keep the default httpx error shape, or use
// WithErrorHandler to intercept handler errors before they reach fiber.
func DefaultErrorHandler(ctx fiber.Ctx, err error) error {
	status, body := httpx.RenderError(normalizeFiberError(err))
	return ctx.Status(status).JSON(body)
}

func normalizeFiberError(err error) error {
	var fe *fiber.Error
	if errors.As(err, &fe) {
		return httpx.NewError(int32(fe.Code), 0, "", err)
	}
	return err
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

// WithAddr sets the listen address. It is the framework-neutral equivalent of
// WithListen, present on every adapter.
func WithAddr(addr string) Option {
	return WithListen(addr)
}

// WithErrorHandler installs a framework-neutral error handler. Errors
// returned by httpx handlers and middleware are rendered through it with a
// real httpx.Context instead of reaching fiber's ErrorHandler. This works
// regardless of how the fiber.App was constructed, since fiber.Config cannot
// be changed after fiber.New.
func WithErrorHandler(errHandler httpx.ErrorHandler) Option {
	return func(conf *Config) {
		conf.errHandler = errHandler
	}
}

type Engine struct {
	engine     *fiber.App
	listen     func(*fiber.App) error
	errHandler httpx.ErrorHandler
	running    atomic.Bool
}

func New(opts ...Option) httpx.Engine {
	conf := NewConfig(opts...)
	engine := &Engine{
		engine:     conf.engine,
		listen:     conf.listen,
		errHandler: conf.errHandler,
	}
	engine.running.Store(false)
	return engine
}

func (e *Engine) Use(middlewares ...httpx.Middleware) {
	for _, middleware := range middlewares {
		e.engine.Use(adaptMiddleware(middleware, e.errHandler))
	}
}

func (e *Engine) Group(prefix string, m ...httpx.Middleware) httpx.Router {
	return &Router{
		basePath:    joinPaths("/", prefix),
		group:       e.engine.Group(prefix),
		middlewares: cloneMiddlewares(nil, m...),
		errHandler:  e.errHandler,
	}
}

func (e *Engine) Start() error {
	e.running.Store(true)
	defer e.running.Store(false)
	return e.listen(e.engine)
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

// Do serves req in-process through the fiber engine and returns the buffered
// response. It implements httpx.TestRequester.
func (e *Engine) Do(req *http.Request) (*http.Response, error) {
	return e.engine.Test(req)
}
