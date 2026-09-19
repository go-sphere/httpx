package ginx

import (
	"context"
	"errors"
	"net"
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

// WithErrorHandler installs a framework-neutral error handler. The handler
// receives a real httpx.Context backed by the gin context, so the same
// error-rendering code can be shared across all adapters.
//
// The gin context is aborted afterwards unless the handler already did it:
// the handler owns the response, and letting gin continue into the remaining
// chain would let a later layer write over the error body.
func WithErrorHandler(errHandler httpx.ErrorHandler) Option {
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

// WithNativeErrorHandler installs gin's own error handler shape. Use it when
// the handler needs the *gin.Context directly; WithErrorHandler is the
// portable option that every adapter accepts.
func WithNativeErrorHandler(errHandler ErrorHandler) Option {
	return func(conf *Config) {
		conf.errHandler = errHandler
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

// WithDefaultMiddleware enables Gin's default Logger and Recovery middleware.
// Without this option the engine starts with no middleware, matching the other adapters.
func WithDefaultMiddleware() Option {
	return func(conf *Config) {
		conf.defaultMiddleware = true
	}
}

// WithTrustedProxies sets the uniform trusted-proxy policy for ClientIP:
// X-Forwarded-For is honored only when the direct peer is inside the given
// IPs/CIDRs, and an empty list ignores forwarding headers entirely (gin's
// default trusts every peer). Invalid entries panic at construction time.
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
	engine     *gin.Engine
	server     *http.Server
	errHandler ErrorHandler
	// chain is the engine scope, referenced by every group created from this
	// engine; see Router.Use.
	chain *httpx.MiddlewareChain
	// notFound and notAllowed carry the engine chain into the unmatched-path
	// answers; see installRouteFallback.
	notFound   *httpx.MiddlewareFallback
	notAllowed *httpx.MiddlewareFallback
	running    atomic.Bool
	closed     atomic.Bool
}

// New constructs a gin-backed Engine using core options.
func New(opts ...Option) httpx.Engine {
	conf := NewConfig(opts...)
	if conf.defaultMiddleware {
		conf.engine.Use(gin.Logger(), gin.Recovery())
	}
	if conf.setTrustedProxies {
		var proxies []string
		if len(conf.trustedProxies) > 0 {
			proxies = conf.trustedProxies
		}
		if err := conf.engine.SetTrustedProxies(proxies); err != nil {
			panic(err)
		}
	}
	conf.server.Handler = conf.engine
	engine := &Engine{
		engine:     conf.engine,
		server:     conf.server,
		errHandler: conf.errHandler,
		chain:      httpx.NewMiddlewareChain(),
	}
	engine.installRouteFallback()
	return engine
}

// installRouteFallback makes gin answer a request no route handled through the
// configured error handler instead of its own plain-text bodies, so 404 and 405
// read the same on every adapter. gin also has to be told to look for a 405 at
// all: with HandleMethodNotAllowed off (its default) a path that exists under
// another method is reported as 404.
//
// Native middleware keeps running for these requests — gin composes NoRoute and
// NoMethod on top of engine.Handlers — and engine-scope httpx middleware reaches
// them through the fallbacks built here, since an unmatched request never
// reaches a route's chain. A group's layers are deliberately absent.
//
// Both handlers are installed unconditionally, so a NoRoute or NoMethod set on
// an engine before it is passed to WithEngine is replaced. Installing your own
// after ginx.New has returned wins outright, because gin's setters replace
// rather than append; that is the supported way to keep a custom fallback.
func (e *Engine) installRouteFallback() {
	e.engine.HandleMethodNotAllowed = true
	e.notFound = httpx.NewMiddlewareFallback(e.chain,
		staticLeaf(httpx.NewNotFoundError(http.StatusText(http.StatusNotFound))))
	// gin has already written the Allow header required by RFC 7231.
	e.notAllowed = httpx.NewMiddlewareFallback(e.chain,
		staticLeaf(httpx.NewError(http.StatusMethodNotAllowed, 0,
			http.StatusText(http.StatusMethodNotAllowed), nil)))
	e.engine.NoRoute(e.fallbackHandler(e.notFound))
	e.engine.NoMethod(e.fallbackHandler(e.notAllowed))
}

// staticLeaf is the innermost handler of an unmatched-path chain: it reports the
// error the adapter would have rendered had no middleware been registered. One
// value per Engine, built in New, which is what lets the composition around it be
// cached.
func staticLeaf(err error) httpx.Handler {
	return func(httpx.Context) error { return err }
}

// fallbackHandler runs the engine chain and renders whatever comes back out of
// it. A layer that answers the request itself — a CORS preflight for a path no
// route matched, a single-page-app rewrite — returns nil and no error is
// rendered, which is the same rule a route's chain follows.
//
// The error is deliberately not added to gin's error list here, unlike on the
// route path: gin has already recorded 404/405 and the fallback is the last
// handler in the chain, so the only effect would be to make a native middleware
// that inspects gin's errors report a failure for every unmatched path.
func (e *Engine) fallbackHandler(fb *httpx.MiddlewareFallback) gin.HandlerFunc {
	return func(gc *gin.Context) {
		err := fb.Handler()(newGinContext(gc))
		if err == nil {
			return
		}
		e.errHandler(gc, err)
		commitErrorStatus(gc, err)
	}
}

// Use registers httpx middleware on the engine, implementing
// httpx.MiddlewareScope. See Router.Use for the ordering rules.
//
// Engine scope is the one scope whose middleware also covers the paths no route
// matched; see installRouteFallback.
func (e *Engine) Use(m ...httpx.Middleware) {
	e.chain.Use(m...)
}

// UseNative registers native gin middleware on the engine. See Router.UseNative
// for why a gin.HandlerFunc can only be mounted this way.
func (e *Engine) UseNative(handlers ...gin.HandlerFunc) {
	e.engine.Use(handlers...)
}

func (e *Engine) Group(prefix string, m ...httpx.Middleware) httpx.Router {
	sub := e.chain.Sub()
	sub.Use(m...)
	return &Router{
		group:      e.engine.Group(prefix),
		errHandler: e.errHandler,
		chain:      sub,
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

// closeNotifyRecorder augments httptest.ResponseRecorder with the legacy
// http.CloseNotifier interface. gin's responseWriter forwards CloseNotify to
// the underlying writer unconditionally, so handlers that probe it (e.g.
// httputil.ReverseProxy when the request context has no Done channel) would
// panic on a bare recorder. The channel never fires: in-process dispatch has
// no connection that could drop.
type closeNotifyRecorder struct {
	*httptest.ResponseRecorder
	done chan bool
}

func (r *closeNotifyRecorder) CloseNotify() <-chan bool { return r.done }

// Do serves req in-process through the gin engine and returns the buffered
// response. It implements httpx.TestRequester.
func (e *Engine) Do(req *http.Request) (*http.Response, error) {
	rr := httptest.NewRecorder()
	e.engine.ServeHTTP(&closeNotifyRecorder{ResponseRecorder: rr, done: make(chan bool)}, req)
	return rr.Result(), nil
}
