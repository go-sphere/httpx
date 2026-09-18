package fiberx

import (
	"context"
	"errors"
	"net"
	"net/http"
	"slices"
	"sync/atomic"

	"github.com/go-sphere/httpx"
	"github.com/gofiber/fiber/v3"
)

var (
	_ httpx.Engine           = (*Engine)(nil)
	_ httpx.InterceptorScope = (*Engine)(nil)
)

type Config struct {
	engine            *fiber.App
	listen            func(*fiber.App) error
	errHandler        httpx.ErrorHandler
	trustedProxies    []string
	setTrustedProxies bool
}

type Option func(*Config)

func NewConfig(opts ...Option) *Config {
	conf := Config{}
	for _, opt := range opts {
		opt(&conf)
	}
	if conf.engine == nil {
		fiberConf := fiber.Config{
			ErrorHandler: DefaultErrorHandler,
			// Fiber keeps the raw path by default, so route parameters would
			// come back percent-encoded where gin, echo and hertz decode them.
			// An engine supplied through WithEngine keeps its own setting —
			// fiber.Config is immutable after fiber.New — so the context
			// decodes route parameters itself when this is off (see
			// fiberContext.paramValue and BindURI).
			UnescapePath: true,
		}
		if conf.setTrustedProxies && len(conf.trustedProxies) > 0 {
			fiberConf.ProxyHeader = fiber.HeaderXForwardedFor
			fiberConf.TrustProxy = true
			// Without IP validation fiber returns the raw joined header value
			// instead of walking the chain right-to-left past trusted hops.
			fiberConf.EnableIPValidation = true
			fiberConf.TrustProxyConfig = fiber.TrustProxyConfig{
				Proxies: conf.trustedProxies,
			}
		}
		conf.engine = fiber.New(fiberConf)
	} else {
		if conf.setTrustedProxies {
			// fiber.Config is immutable after fiber.New; silently ignoring a
			// security option would be worse than failing loudly.
			panic("fiberx: WithTrustedProxies requires the engine to be constructed by fiberx; configure TrustProxy/TrustProxyConfig on your own fiber.Config instead")
		}
	}
	if conf.errHandler == nil {
		// Render handler and middleware errors in the adapter rather than
		// letting them unwind into fiber.Config.ErrorHandler: that field can
		// only be set when this adapter builds the app, so an engine passed
		// through WithEngine would otherwise answer with fiber's plain-text
		// default — leaking err.Error() and reporting every status as 500.
		conf.errHandler = defaultHTTPXErrorHandler
	}
	if conf.listen == nil {
		conf.listen = func(app *fiber.App) error {
			return app.Listen(":8080")
		}
	}
	return &conf
}

// defaultHTTPXErrorHandler is DefaultErrorHandler on the framework-neutral
// side: same status and body, reached with an httpx.Context.
func defaultHTTPXErrorHandler(ctx httpx.Context, err error) {
	status, body := httpx.RenderError(normalizeFiberError(err))
	_ = ctx.JSON(status, body)
}

// DefaultErrorHandler renders errors with the standard httpx error body. It
// understands *fiber.Error (framework 404/405/... errors) so their status
// codes are preserved instead of being reported as 500.
//
// Errors from httpx handlers and middleware no longer reach it: the adapter
// renders them itself (see defaultHTTPXErrorHandler), because fiber.Config is
// immutable after fiber.New and an engine supplied through WithEngine would
// otherwise answer with fiber's plain-text default. This handler is what the
// adapter-built engine installs for the errors fiber raises on its own —
// unmatched routes, a rejected method, an oversized body. Set it as your
// fiber.Config's ErrorHandler to get the same shape for those on your own app.
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

// WithTrustedProxies sets the uniform trusted-proxy policy for ClientIP:
// X-Forwarded-For is honored only when the direct peer is inside the given
// IPs/CIDRs, and an empty list ignores forwarding headers entirely (which is
// already fiber's default). It only takes effect when the engine is
// constructed by this adapter; combined with WithEngine it panics, because
// fiber.Config cannot be changed after fiber.New. Invalid entries panic at
// construction time.
func WithTrustedProxies(proxies ...string) Option {
	return func(conf *Config) {
		if _, err := httpx.ParseCIDRs(proxies); err != nil {
			panic(err)
		}
		conf.trustedProxies = proxies
		conf.setTrustedProxies = true
	}
}

type Engine struct {
	engine     *fiber.App
	listen     func(*fiber.App) error
	errHandler httpx.ErrorHandler
	// interceptors are inherited by every group created from this engine; see
	// Router.UseInterceptor.
	interceptors []httpx.Interceptor
	running      atomic.Bool
	closed       atomic.Bool
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

// UseInterceptor registers composed middleware on the engine, implementing
// httpx.InterceptorScope. See Router.UseInterceptor for the ordering rules.
func (e *Engine) UseInterceptor(m ...httpx.Interceptor) {
	if len(m) == 0 {
		return
	}
	e.interceptors = append(slices.Clone(e.interceptors), m...)
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
	if e.closed.Load() {
		// fiber would happily restart after Shutdown; refuse for the uniform
		// single-use Engine contract.
		return httpx.ErrEngineClosed
	}
	e.running.Store(true)
	defer e.running.Store(false)
	return e.listen(e.engine)
}

func (e *Engine) Stop(ctx context.Context) error {
	e.closed.Store(true)
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
