package hertzx

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/textproto"
	"sync/atomic"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/middlewares/server/recovery"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/go-sphere/httpx"
)

var _ httpx.Engine = (*Engine)(nil)

type ErrorHandler func(ctx context.Context, rc *app.RequestContext, err error)

type Config struct {
	engine            *server.Hertz
	addr              string
	errHandler        ErrorHandler
	defaultMiddleware bool
	clientIP          app.ClientIP
}

type Option func(*Config)

func NewConfig(opts ...Option) *Config {
	conf := Config{}
	for _, opt := range opts {
		opt(&conf)
	}
	if conf.engine == nil {
		if conf.addr != "" {
			conf.engine = server.New(server.WithHostPorts(conf.addr))
		} else {
			conf.engine = server.New()
		}
	}
	if conf.errHandler == nil {
		conf.errHandler = DefaultErrorHandler
	}
	return &conf
}

// DefaultErrorHandler is the native error handler installed when no custom
// handler is configured. It renders the standard httpx error body.
func DefaultErrorHandler(ctx context.Context, rc *app.RequestContext, err error) {
	status, body := httpx.RenderError(err)
	rc.JSON(status, body)
	rc.Abort()
}

func WithEngine(engine *server.Hertz) Option {
	return func(conf *Config) {
		conf.engine = engine
	}
}

// WithAddr sets the listen address. It only takes effect when the engine is
// constructed by this adapter; when providing your own engine via WithEngine,
// configure the address with server.WithHostPorts instead.
func WithAddr(addr string) Option {
	return func(conf *Config) {
		conf.addr = addr
	}
}

// WithNativeErrorHandler installs hertz's own error handler shape. Use it when
// the handler needs the *app.RequestContext directly; WithErrorHandler is the
// portable option that every adapter accepts.
func WithNativeErrorHandler(errHandler ErrorHandler) Option {
	return func(conf *Config) {
		conf.errHandler = errHandler
	}
}

// WithErrorHandler installs a framework-neutral error handler. The handler
// receives a real httpx.Context backed by the hertz context, so the same
// error-rendering code can be shared across all adapters.
//
// The hertz context is aborted afterwards unless the handler already did it:
// the handler owns the response, and letting hertz continue into the
// remaining chain would let a later layer write over the error body.
func WithErrorHandler(errHandler httpx.ErrorHandler) Option {
	return func(conf *Config) {
		if errHandler == nil {
			return
		}
		conf.errHandler = func(ctx context.Context, rc *app.RequestContext, err error) {
			errHandler(FromHertz(ctx, rc), err)
			if !rc.IsAborted() {
				rc.Abort()
			}
		}
	}
}

// WithDefaultMiddleware enables Hertz's default Recovery middleware.
// Without this option the engine starts with no middleware, matching the other adapters.
func WithDefaultMiddleware() Option {
	return func(conf *Config) {
		conf.defaultMiddleware = true
	}
}

// WithTrustedProxies sets the uniform trusted-proxy policy for ClientIP:
// X-Forwarded-For / X-Real-IP are honored only when the direct peer is
// inside the given IPs/CIDRs, and an empty list ignores forwarding headers
// entirely (hertz's default trusts every peer). Blank or bracketed entries are
// not accepted. Invalid entries panic at construction time.
func WithTrustedProxies(proxies ...string) Option {
	return func(conf *Config) {
		cidrs, err := httpx.ParseCIDRs(proxies)
		if err != nil {
			panic(err)
		}
		conf.clientIP = app.ClientIPWithOption(app.ClientIPOptions{
			RemoteIPHeaders: []string{"X-Forwarded-For", "X-Real-IP"},
			TrustedCIDRs:    cidrs,
		})
	}
}

type Engine struct {
	engine     *server.Hertz
	errHandler ErrorHandler
	// chain is the engine scope, referenced by every group created from this
	// engine; see Router.Use.
	chain *httpx.MiddlewareChain
	// notFound and notAllowed carry the engine chain into the unmatched-path
	// answers; see installRouteFallback.
	notFound   *httpx.MiddlewareFallback
	notAllowed *httpx.MiddlewareFallback
	clientIP   app.ClientIP
	running    atomic.Bool
	closed     atomic.Bool
}

func New(opts ...Option) httpx.Engine {
	conf := NewConfig(opts...)
	if conf.defaultMiddleware {
		conf.engine.Use(recovery.Recovery())
	}
	if conf.clientIP != nil {
		conf.engine.SetClientIPFunc(conf.clientIP)
	}
	engine := &Engine{
		engine:     conf.engine,
		errHandler: conf.errHandler,
		clientIP:   conf.clientIP,
		chain:      httpx.NewMiddlewareChain(),
	}
	engine.installRouteFallback()
	engine.running.Store(false)
	return engine
}

// installRouteFallback makes hertz answer a request no route handled through
// the configured error handler instead of its own plain-text bodies, so 404 and
// 405 read the same on every adapter.
//
// hertz also has to be told to look for a 405 at all: with its default
// HandleMethodNotAllowed off, a path that exists under another method is
// reported as 404. The flag lives in the options struct GetOptions hands back
// by pointer, the only way to reach it on an engine supplied through WithEngine
// (server.WithHandleMethodNotAllowed is a server.New option), and hertz reads it
// per request, so setting it here is in time.
//
// Native middleware keeps running for these requests, since hertz composes
// NoRoute and NoMethod on top of the engine's own handler chain, and engine-scope
// httpx middleware reaches them through the fallbacks built here. A group's
// layers are deliberately absent.
//
// Precedence follows ginx: both handlers are installed unconditionally, so a
// NoRoute or NoMethod set before WithEngine is replaced and one set after
// hertzx.New wins, because hertz's setters replace rather than append.
func (e *Engine) installRouteFallback() {
	e.engine.GetOptions().HandleMethodNotAllowed = true
	e.notFound = httpx.NewMiddlewareFallback(e.chain,
		staticLeaf(httpx.NewNotFoundError(http.StatusText(http.StatusNotFound))))
	// Unlike gin, hertz does not write the Allow header RFC 7231 requires, and it
	// does not expose which methods it matched, so there is nothing to write it
	// from here.
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
// The error is deliberately not added to hertz's error list here, unlike on the
// route path: hertz has already recorded 404/405 and the fallback is the last
// handler in the chain, so the only effect would be to make a native middleware
// that inspects hertz's errors report a failure for every unmatched path.
func (e *Engine) fallbackHandler(fb *httpx.MiddlewareFallback) app.HandlerFunc {
	return func(ctx context.Context, rc *app.RequestContext) {
		err := fb.Handler()(newHertzContext(ctx, rc))
		if err == nil {
			return
		}
		e.errHandler(ctx, rc, err)
		commitErrorStatus(rc, err)
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

// UseNative registers native hertz middleware on the engine. See
// Router.UseNative for why an app.HandlerFunc can only be mounted this way.
func (e *Engine) UseNative(handlers ...app.HandlerFunc) {
	e.engine.Use(handlers...)
}

func (e *Engine) Group(prefix string, m ...httpx.Middleware) httpx.Router {
	sub := e.chain.Sub()
	sub.Use(m...)
	return &Router{
		group:      e.engine.Group(prefix),
		chain:      sub,
		errHandler: e.errHandler,
	}
}

func (e *Engine) Start() error {
	if e.closed.Load() {
		return httpx.ErrEngineClosed
	}
	e.running.Store(true)
	defer e.running.Store(false)
	return e.engine.Run()
}

// Stop follows the same semantic as httpx.Close, which the net/http-backed
// adapters share: drain gracefully, and when that fails force the transport
// closed so nothing is left accepting. A drain the caller's context cut short
// reports success — the server is down, the drain degraded — while any other
// failure is force-closed too but reported.
//
// What hertz cannot do is cut live connections: Engine.Close is Shutdown with
// an already-expired context, so it closes the listener immediately but still
// leaves a request in flight to finish on its own. Closing the listener is the
// part a caller asking for a forced stop actually needs, and claiming more than
// that would be wrong.
func (e *Engine) Stop(ctx context.Context) error {
	e.closed.Store(true)
	if ctx == nil {
		ctx = context.Background()
	}
	running := e.running.Load()
	// The engine is down whatever Shutdown reports, so the running flag has to
	// fall with it. Storing it only on a nil error left IsRunning reporting true
	// for the rest of the process after a stop that timed out.
	defer e.running.Store(false)
	err := e.engine.Shutdown(ctx)
	if err == nil || !running {
		// Nothing was ever serving: there is no transport to force closed, and
		// hertz's own "not running" error is the honest answer.
		return err
	}
	closeErr := e.engine.Close()
	if ctx.Err() != nil {
		return closeErr
	}
	if closeErr != nil {
		return errors.Join(err, closeErr)
	}
	return err
}

// IsRunning returns true if the server is currently running.
func (e *Engine) IsRunning() bool {
	return e.running.Load()
}

// Do serves req in-process through the hertz engine and returns the buffered
// response. It implements httpx.TestRequester.
func (e *Engine) Do(req *http.Request) (*http.Response, error) {
	urlStr := req.URL.String()
	if !req.URL.IsAbs() {
		urlStr = "http://" + req.Host + req.URL.RequestURI()
		if req.Host == "" {
			urlStr = "http://localhost" + req.URL.RequestURI()
		}
	}

	hctx := e.engine.NewContext()
	if e.clientIP != nil {
		// Pooled contexts get this in allocateContext; NewContext does not.
		hctx.SetClientIPFunc(e.clientIP)
	}
	hctx.Request.Header.SetMethod(req.Method)
	hctx.Request.SetRequestURI(urlStr)
	if req.Body != nil {
		body, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		if len(body) > 0 {
			hctx.Request.SetBodyStream(bytes.NewReader(body), len(body))
		}
	}
	for key, values := range req.Header {
		for _, value := range values {
			hctx.Request.Header.Add(key, value)
		}
	}

	e.engine.ServeHTTP(req.Context(), hctx)

	header := make(http.Header)
	hctx.Response.Header.VisitAll(func(k, v []byte) {
		key := textproto.CanonicalMIMEHeaderKey(string(k))
		if key == "Set-Cookie" {
			return
		}
		header.Add(key, string(v))
	})
	for _, setCookie := range hctx.Response.Header.GetAll("Set-Cookie") {
		// Hertz returns a single empty string when the response sets no
		// cookie, which would otherwise surface as a malformed "Set-Cookie:"
		// header that no client ever receives over the wire.
		if setCookie == "" {
			continue
		}
		header.Add("Set-Cookie", setCookie)
	}

	body := bytes.Clone(hctx.Response.Body())
	return &http.Response{
		Status:        http.StatusText(hctx.Response.StatusCode()),
		StatusCode:    hctx.Response.StatusCode(),
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		Header:        header,
		Body:          io.NopCloser(bytes.NewReader(body)),
		ContentLength: int64(len(body)),
		Request:       req,
	}, nil
}
