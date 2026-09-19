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
// Errors from httpx handlers and middleware do not reach it: the adapter renders
// them itself (see defaultHTTPXErrorHandler), because fiber.Config is immutable
// after fiber.New and an engine supplied through WithEngine would otherwise
// answer with fiber's plain-text default. This handler is what the adapter-built
// engine installs for the errors fiber raises on its own — unmatched routes, a
// rejected method, an oversized body. Set it as your fiber.Config's ErrorHandler
// to get the same shape for those on your own app.
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
	// chain is the engine scope, referenced by every group created from this
	// engine; see Router.Use. notFound and notAllowed carry it into the
	// unmatched-path answers, which is the one place a group's chain must not
	// reach.
	chain      *httpx.MiddlewareChain
	notFound   *httpx.MiddlewareFallback
	notAllowed *httpx.MiddlewareFallback
	running    atomic.Bool
	closed     atomic.Bool
}

// routeFallback renders the errors fiber raises for itself — an unmatched path,
// a rejected method — through the engine's httpx.ErrorHandler with a real
// httpx.Context, the same way a route that returns an error is rendered.
//
// Without it those answers come from fiber.Config.ErrorHandler, which this
// adapter can only set when it builds the app: fiber.Config is immutable after
// fiber.New, so an app supplied through WithEngine would answer an unmatched
// path with fiber's plain-text default instead of the shared error body. It is
// registered before any route, so it is the outermost layer, and errors a route
// already dealt with never reach it (handleFiberError returns nil for those).
//
// It is also where engine-scope middleware reaches a path no route matched; a
// group's chain must not. The composition is resolved here rather than captured,
// because this handler is installed by New, before any Use call.
//
// Only fiber's own 404 and 405 are treated as unmatched, matched by identity
// against the package values fiber raises for itself.
func (e *Engine) routeFallback() fiber.Handler {
	return func(ctx fiber.Ctx) error {
		err := ctx.Next()
		if err == nil {
			return nil
		}
		if fb := e.unmatchedFallback(err); fb != nil {
			// Marked before the chain runs, because the layers inside it read
			// FullPath and must not be told a route matched; see
			// unmatchedRouteKey.
			markUnmatchedRoute(ctx)
			raw := err
			if err = fb.Handler()(newFiberContext(ctx)); err == nil {
				// A layer answered the unmatched path itself — or returned without
				// answering, in which case the fallback's own status is still
				// owed; see commitErrorStatus.
				commitErrorStatus(ctx, raw)
				return nil
			}
		}
		if e.errHandler == nil || responseDecided(ctx) {
			return err
		}
		e.errHandler(newFiberContext(ctx), normalizeFiberError(err))
		// An error handler that rendered nothing still owes the error's status:
		// fiber would otherwise send the 200 it starts every response with, so
		// a path no route matched would answer 200. See commitErrorStatus.
		commitErrorStatus(ctx, err)
		return nil
	}
}

// unmatchedFallback returns the engine chain for the unmatched-path answer err
// belongs to, or nil when err is not one fiber raised for an unmatched path.
//
// Identity, not status: fiber raises these exact package values for a path no
// route matched, while a route that chose to return fiber.NewError(404) built its
// own and is not an unmatched path.
func (e *Engine) unmatchedFallback(err error) *httpx.MiddlewareFallback {
	switch {
	case errors.Is(err, fiber.ErrNotFound):
		return e.notFound
	case errors.Is(err, fiber.ErrMethodNotAllowed):
		return e.notAllowed
	default:
		return nil
	}
}

func New(opts ...Option) httpx.Engine {
	conf := NewConfig(opts...)
	engine := &Engine{
		engine:     conf.engine,
		listen:     conf.listen,
		errHandler: conf.errHandler,
		chain:      httpx.NewMiddlewareChain(),
	}
	// The leaf reports the error fiber raised, which is what the configured
	// httpx.ErrorHandler received before the engine chain covered unmatched
	// paths: one value per Engine, so the composition around it can be cached.
	engine.notFound = httpx.NewMiddlewareFallback(engine.chain, staticLeaf(fiber.ErrNotFound))
	engine.notAllowed = httpx.NewMiddlewareFallback(engine.chain, staticLeaf(fiber.ErrMethodNotAllowed))
	conf.engine.Use(engine.routeFallback())
	engine.running.Store(false)
	return engine
}

// staticLeaf is the innermost handler of an unmatched-path chain: it reports the
// error the adapter would have rendered had no middleware been registered.
func staticLeaf(err error) httpx.Handler {
	return func(httpx.Context) error { return err }
}

// Use registers httpx middleware on the engine, implementing
// httpx.MiddlewareScope. See Router.Use for the ordering rules.
//
// Engine scope is the one scope whose middleware also covers the paths no route
// matched; see routeFallback.
func (e *Engine) Use(m ...httpx.Middleware) {
	e.chain.Use(m...)
}

// UseNative registers native fiber middleware on the engine. See
// Router.UseNative for why a fiber.Handler can only be mounted this way.
func (e *Engine) UseNative(handlers ...fiber.Handler) {
	for _, h := range handlers {
		e.engine.Use(h)
	}
}

func (e *Engine) Group(prefix string, m ...httpx.Middleware) httpx.Router {
	sub := e.chain.Sub()
	sub.Use(m...)
	return &Router{
		basePath:   joinPaths("/", prefix),
		group:      e.engine.Group(prefix),
		chain:      sub,
		errHandler: e.errHandler,
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

// Stop aims at the same semantic as httpx.Close, which the net/http-backed
// adapters share: a drain the caller's context cut short still leaves the
// server down and reports success.
//
// fasthttp does most of that already — ShutdownWithContext closes every
// listener before it starts waiting, so the server stops accepting whatever the
// context does. What it does not offer is a forced close for the connections
// still in flight: fasthttp.Server has no Close, and fiber hands out no listener
// to close behind its back. So a context that expired is reported as success —
// the listener is down, which is the part a forced stop is asked for — rather
// than pretending the connections were cut, which httpxtest.Caps declares with
// ForcedStopCutsConnections = false.
//
// A shutdown that genuinely failed is a different answer and keeps its error;
// see classifyShutdownError.
func (e *Engine) Stop(ctx context.Context) error {
	e.closed.Store(true)
	if ctx == nil {
		ctx = context.Background()
	}
	// The listener is closed whatever this returns, so the running flag has to
	// fall with it. Storing it only on a nil error left IsRunning reporting true
	// for the rest of the process after a stop that timed out.
	defer e.running.Store(false)
	return classifyShutdownError(ctx, e.engine.ShutdownWithContext(ctx))
}

// classifyShutdownError decides which of fasthttp's two failure modes the caller
// is told about.
//
// ShutdownWithContext returns one of exactly two things when it fails: the
// caller's own context error, because the connections in flight outlived the
// deadline, or whatever closing the listeners produced. The first is the
// degraded stop above — the listeners are already closed, so the server is down
// and success is the honest report, as it is for httpx.Close. The second means a
// listener did not close and the socket may still be bound, which Stop must
// never report as a successful stop, so it is returned unchanged.
//
// ctx.Err() alone cannot separate them: fasthttp closes the listeners and
// collects their errors before it consults the context, so a listener that
// failed to close while the caller's deadline happened to be spent comes back
// as nil. One case stays out of reach — on the timed-out path fasthttp returns
// ctx.Err() and drops the collected listener error, so a close failure
// coinciding with an expired deadline cannot be surfaced; reporting success
// there is the degraded answer the deadline alone earns.
func classifyShutdownError(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	if ctxErr := ctx.Err(); ctxErr != nil && errors.Is(err, ctxErr) {
		return nil
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
