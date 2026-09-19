package echox

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"

	"github.com/go-sphere/httpx"
	"github.com/labstack/echo/v4"
)

var _ httpx.Engine = (*Engine)(nil)

type Config struct {
	engine      *echo.Echo
	server      *http.Server
	errHandler  httpx.ErrorHandler
	ipExtractor echo.IPExtractor
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
	// Install the adapter default error handler unless something more specific
	// owns the slot. Comparing against echo's default handler makes
	// echox.New(WithEngine(echo.New())) behave the same as echox.New().
	//
	// Precedence for echo.Echo.HTTPErrorHandler, highest first:
	//
	//  1. WithErrorHandler — New installs it, replacing whatever is here, so
	//     httpx owns every error echo renders rather than only handler errors.
	//  2. a handler the caller set on their own echo.Echo before WithEngine.
	//  3. this adapter's DefaultErrorHandler.
	//
	// Both sites read this list: New owns the top entry, and this skips the slot
	// entirely when New will fill it.
	if conf.errHandler == nil && (conf.engine.HTTPErrorHandler == nil || isEchoDefaultErrorHandler(conf.engine)) {
		conf.engine.HTTPErrorHandler = DefaultErrorHandler
	}
	if conf.server == nil {
		conf.server = &http.Server{
			Addr: ":8080",
		}
	}
	return conf
}

func isEchoDefaultErrorHandler(e *echo.Echo) bool {
	if e.HTTPErrorHandler == nil {
		return false
	}
	return reflect.ValueOf(e.HTTPErrorHandler).Pointer() == reflect.ValueOf(e.DefaultHTTPErrorHandler).Pointer()
}

// DefaultErrorHandler renders errors with the standard httpx error body.
// It understands *echo.HTTPError (framework 404/405/... errors) so their
// status codes are preserved instead of being reported as 500.
//
// Across the repository DefaultErrorHandler is always this adapter's default
// handler in its native shape, which is why the five signatures differ and why
// it is not interchangeable across adapters. WithErrorHandler, which takes
// httpx.ErrorHandler everywhere, is the portable surface.
func DefaultErrorHandler(err error, c echo.Context) {
	if c.Response().Committed {
		return
	}
	status, body := httpx.RenderError(normalizeEchoError(err))
	// Through the adapter's own context so the JSON Content-Type carries the
	// charset the other four adapters write. No wildcard table: this is a
	// package-level function with no engine behind it, and rendering an error
	// body reads no route parameters.
	_ = newEchoContext(c, nil).JSON(status, body)
}

func normalizeEchoError(err error) error {
	var he *echo.HTTPError
	if errors.As(err, &he) {
		return httpx.NewError(int32(he.Code), 0, "", err)
	}
	return err
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

// WithAddr sets the listen address, creating the http.Server when none was
// supplied. It is the framework-neutral option present on every adapter.
func WithAddr(addr string) Option {
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

// WithErrorHandler installs a framework-neutral error handler. Errors
// returned by httpx handlers and middleware are rendered through it with a
// real httpx.Context instead of echo's HTTPErrorHandler.
func WithErrorHandler(errHandler httpx.ErrorHandler) Option {
	return func(conf *Config) {
		conf.errHandler = errHandler
	}
}

// WithTrustedProxies sets the uniform trusted-proxy policy for ClientIP:
// X-Forwarded-For is honored only when the direct peer is inside the given
// IPs/CIDRs, and an empty list ignores forwarding headers entirely (echo's
// default trusts every peer). Invalid entries panic at construction time.
func WithTrustedProxies(proxies ...string) Option {
	return func(conf *Config) {
		cidrs, err := httpx.ParseCIDRs(proxies)
		if err != nil {
			panic(err)
		}
		if len(cidrs) == 0 {
			conf.ipExtractor = echo.ExtractIPDirect()
			return
		}
		options := []echo.TrustOption{
			// Trust exactly the configured list, not echo's implicit
			// loopback/link-local/private defaults.
			echo.TrustLoopback(false),
			echo.TrustLinkLocal(false),
			echo.TrustPrivateNet(false),
		}
		for _, cidr := range cidrs {
			options = append(options, echo.TrustIPRange(cidr))
		}
		conf.ipExtractor = echo.ExtractIPFromXFFHeader(options...)
	}
}

type Engine struct {
	engine     *echo.Echo
	server     *http.Server
	errHandler httpx.ErrorHandler
	// nativeErrHandler is whatever occupied echo's HTTPErrorHandler slot after
	// NewConfig applied the precedence rules, kept so installErrorHandler can
	// wrap rather than replace it.
	nativeErrHandler echo.HTTPErrorHandler
	// chain is the engine scope, referenced by every group created from this
	// engine; see Router.Use. notFound and notAllowed carry it into the
	// unmatched-path answers, which is the one place a group's chain must not
	// reach.
	chain      *httpx.MiddlewareChain
	notFound   *httpx.MiddlewareFallback
	notAllowed *httpx.MiddlewareFallback
	// wildcards is this engine's named-wildcard table; every Router it makes
	// shares it, and no other engine can see it. See wildcardTable.
	wildcards *wildcardTable
	running   atomic.Bool
	closed    atomic.Bool
}

func New(opts ...Option) httpx.Engine {
	conf := NewConfig(opts...)
	wildcards := &wildcardTable{}
	conf.engine.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			err := next(c)
			if err == nil && !c.Response().Committed {
				c.Response().WriteHeader(c.Response().Status)
			}
			return err
		}
	})
	if conf.ipExtractor != nil {
		conf.engine.IPExtractor = conf.ipExtractor
	}
	conf.server.Handler = conf.engine
	engine := &Engine{
		engine:           conf.engine,
		server:           conf.server,
		errHandler:       conf.errHandler,
		nativeErrHandler: conf.engine.HTTPErrorHandler,
		chain:            httpx.NewMiddlewareChain(),
		wildcards:        wildcards,
	}
	// The leaf reports the error echo raised, which is what the configured
	// httpx.ErrorHandler received before the engine chain covered unmatched
	// paths: one value per Engine, so the composition around it can be cached.
	engine.notFound = httpx.NewMiddlewareFallback(engine.chain, staticLeaf(echo.ErrNotFound))
	engine.notAllowed = httpx.NewMiddlewareFallback(engine.chain, staticLeaf(echo.ErrMethodNotAllowed))
	engine.installErrorHandler()
	engine.running.Store(false)
	return engine
}

// staticLeaf is the innermost handler of an unmatched-path chain: it reports the
// error the adapter would have rendered had no middleware been registered.
func staticLeaf(err error) httpx.Handler {
	return func(httpx.Context) error { return err }
}

// installErrorHandler owns echo's HTTPErrorHandler slot, for two reasons that
// have to be served by the same function.
//
// The first: errors echo raises for itself — an unmatched path, a rejected
// method — never pass through the router wrapper, so without this a configured
// httpx.ErrorHandler would own every response except the ones the application
// did not route.
//
// The second: this is the only place engine-scope middleware can reach an
// unmatched path on echo. Only the two errors echo raises for itself get the
// chain; anything else reaching this slot came from a route, whose own chain has
// already run, and running the engine's a second time would double it.
//
// It wraps rather than replaces whatever NewConfig left in the slot, which
// preserves the configured precedence and extends the unmatched-path coverage to
// the default configuration too. Setting Echo.HTTPErrorHandler after echox.New
// has returned still wins outright, at the cost of that coverage.
func (e *Engine) installErrorHandler() {
	e.engine.HTTPErrorHandler = func(err error, c echo.Context) {
		if c.Response().Committed {
			return
		}
		if fb := e.unmatchedFallback(err); fb != nil {
			// Marked before the chain runs, because the layers inside it read
			// FullPath and must not be told a route accepted the request; see
			// unmatchedRouteKey.
			c.Set(unmatchedRouteKey, true)
			if chained := fb.Handler()(newEchoContext(c, e.wildcards)); chained == nil {
				// A layer answered the unmatched path itself. It still owes a
				// status if all it did was set one.
				commitErrorStatus(c.Response(), err)
				return
			}
		}
		if e.errHandler != nil {
			e.errHandler(newEchoContext(c, e.wildcards), normalizeEchoError(err))
			// The handler may have rendered nothing; commit here, since nothing
			// runs below and echo does not commit on its own. This is the path
			// an unmatched route takes, where Response.Status is still 200 —
			// see commitErrorStatus.
			commitErrorStatus(c.Response(), err)
			return
		}
		if e.nativeErrHandler != nil {
			e.nativeErrHandler(err, c)
		}
	}
}

// unmatchedFallback returns the engine chain for the unmatched-path answer err
// belongs to, or nil when err is not one echo raised for an unmatched path.
//
// Identity, not status: echo's NotFoundHandler and MethodNotAllowedHandler
// return these exact package values, while a route that chose to return
// echo.NewHTTPError(404) built its own and is not an unmatched path.
func (e *Engine) unmatchedFallback(err error) *httpx.MiddlewareFallback {
	switch {
	case errors.Is(err, echo.ErrNotFound):
		return e.notFound
	case errors.Is(err, echo.ErrMethodNotAllowed):
		return e.notAllowed
	default:
		return nil
	}
}

// Use registers httpx middleware on the engine, implementing
// httpx.MiddlewareScope. See Router.Use for the ordering rules.
//
// Engine scope is the one scope whose middleware also covers the paths no route
// matched; see installErrorHandler.
func (e *Engine) Use(m ...httpx.Middleware) {
	e.chain.Use(m...)
}

// UseNative registers native echo middleware on the engine. See Router.UseNative
// for why an echo.MiddlewareFunc can only be mounted this way.
func (e *Engine) UseNative(middleware ...echo.MiddlewareFunc) {
	e.engine.Use(middleware...)
}

func (e *Engine) Group(prefix string, m ...httpx.Middleware) httpx.Router {
	base := joinPaths("/", prefix)
	sub := e.chain.Sub()
	sub.Use(m...)
	return &Router{
		group:      e.engine.Group(echoGroupPrefix("/", base)),
		basePath:   base,
		errHandler: e.errHandler,
		chain:      sub,
		wildcards:  e.wildcards,
	}
}

func (e *Engine) Start() error {
	if e.closed.Load() {
		return httpx.ErrEngineClosed
	}
	addr := e.server.Addr
	if addr == "" {
		addr = ":http"
	}
	// Bind first so IsRunning only reports true once the listener exists.
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	e.running.Store(true)
	defer e.running.Store(false)
	if err := e.server.Serve(ln); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func (e *Engine) Stop(ctx context.Context) error {
	e.closed.Store(true)
	// httpx.Close force-closes when the graceful drain fails, so the server is
	// down whatever it returns — the running flag has to fall with it. Storing
	// it only on a nil error left IsRunning reporting true for the rest of the
	// process after a stop the caller's deadline cut short.
	defer e.running.Store(false)
	return httpx.Close(ctx, e.server)
}

// IsRunning returns true if the server is currently running.
func (e *Engine) IsRunning() bool {
	return e.running.Load()
}

// Do serves req in-process through the echo engine and returns the buffered
// response. It implements httpx.TestRequester.
func (e *Engine) Do(req *http.Request) (*http.Response, error) {
	rr := httptest.NewRecorder()
	e.engine.ServeHTTP(rr, req)
	return rr.Result(), nil
}
