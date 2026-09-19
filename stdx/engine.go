package stdx

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/go-sphere/httpx"
)

var (
	_ httpx.Engine = (*Engine)(nil)
	_ http.Handler = (*Engine)(nil)
)

type Config struct {
	server         *http.Server
	errHandler     httpx.ErrorHandler
	trustedProxies []*net.IPNet
	// trustProxies distinguishes "not configured" from "configured empty";
	// both ignore forwarding headers, but only the first may change later.
	trustProxies bool
}

type Option func(*Config)

func NewConfig(opts ...Option) *Config {
	conf := &Config{}
	for _, opt := range opts {
		opt(conf)
	}
	if conf.server == nil {
		conf.server = &http.Server{Addr: ":8080"}
	}
	if conf.errHandler == nil {
		conf.errHandler = DefaultErrorHandler
	}
	return conf
}

// DefaultErrorHandler renders errors with the standard httpx error body.
func DefaultErrorHandler(ctx httpx.Context, err error) {
	status, body := httpx.RenderError(err)
	_ = ctx.JSON(status, body)
}

// WithServer supplies the http.Server to serve with. The adapter installs
// itself as its Handler.
func WithServer(server *http.Server) Option {
	return func(conf *Config) { conf.server = server }
}

// WithAddr sets the listen address. It is the framework-neutral option present
// on every adapter.
func WithAddr(addr string) Option {
	return func(conf *Config) {
		if conf.server == nil {
			conf.server = &http.Server{Addr: addr}
		} else {
			conf.server.Addr = addr
		}
	}
}

// WithErrorHandler installs a framework-neutral error handler. Errors returned
// by httpx handlers and middleware are rendered through it.
func WithErrorHandler(errHandler httpx.ErrorHandler) Option {
	return func(conf *Config) { conf.errHandler = errHandler }
}

// WithTrustedProxies sets the uniform trusted-proxy policy for ClientIP:
// X-Forwarded-For is honored only when the direct peer is inside the given
// IPs/CIDRs, and an empty list ignores forwarding headers entirely (which is
// also the default). Invalid entries panic at construction time.
func WithTrustedProxies(proxies ...string) Option {
	return func(conf *Config) {
		cidrs, err := httpx.ParseCIDRs(proxies)
		if err != nil {
			panic(err)
		}
		conf.trustedProxies = cidrs
		conf.trustProxies = true
	}
}

// Engine is the adapter's http.Handler: it owns the route tree, the engine
// middleware chain and the http.Server. Nothing wraps net/http here, so an
// Engine can also be mounted inside another net/http server.
type Engine struct {
	root   *node
	server *http.Server

	errHandler     httpx.ErrorHandler
	trustedProxies []*net.IPNet

	// chain is the engine scope, referenced by every group created from this
	// engine; see Router.Use. notFound and notAllowed carry it into the
	// unmatched-path answers, which is the one place a group's chain must not
	// reach.
	chain      *httpx.MiddlewareChain
	notFound   *httpx.MiddlewareFallback
	notAllowed *httpx.MiddlewareFallback

	pool    sync.Pool
	running atomic.Bool
	closed  atomic.Bool
}

func New(opts ...Option) httpx.Engine {
	conf := NewConfig(opts...)
	engine := &Engine{
		root:           &node{},
		server:         conf.server,
		errHandler:     conf.errHandler,
		trustedProxies: conf.trustedProxies,
		chain:          httpx.NewMiddlewareChain(),
	}
	engine.notFound = httpx.NewMiddlewareFallback(engine.chain,
		staticLeaf(httpx.NewNotFoundError(http.StatusText(http.StatusNotFound))))
	engine.notAllowed = httpx.NewMiddlewareFallback(engine.chain,
		staticLeaf(httpx.NewError(http.StatusMethodNotAllowed, 0,
			http.StatusText(http.StatusMethodNotAllowed), nil)))
	engine.pool.New = func() any {
		ctx := &stdContext{engine: engine}
		ctx.native.c = ctx
		return ctx
	}
	engine.server.Handler = engine
	engine.running.Store(false)
	return engine
}

// Use registers httpx middleware on the engine, implementing
// httpx.MiddlewareScope. See Router.Use for the ordering rules.
//
// Engine scope is the one scope whose middleware also covers the paths no route
// matched; see ServeHTTP.
func (e *Engine) Use(m ...httpx.Middleware) {
	e.chain.Use(m...)
}

func (e *Engine) Group(prefix string, m ...httpx.Middleware) httpx.Router {
	sub := e.chain.Sub()
	sub.Use(m...)
	return &Router{
		engine:   e,
		basePath: joinPaths("/", prefix),
		chain:    sub,
	}
}

// ServeHTTP dispatches one request: resolve the route, run its chain, and
// render an error only when the handlers produced no response of their own.
func (e *Engine) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	ctx, _ := e.pool.Get().(*stdContext)
	ctx.reset(w, req)

	// fallbackStatus is non-zero for a request no route matched, so a chain that
	// swallowed the error still owes that status; see below.
	fallbackStatus := 0
	var handler httpx.Handler
	r, allow := e.root.match(req.Method, req.URL.Path, &ctx.values)
	if r != nil {
		ctx.route = r
		handler = r.handler
	} else {
		// Unmatched paths are outside every group, so only the engine's own
		// middleware runs — the same rule the other adapters inherit. A group's
		// layers must not: a 404 belongs to no group.
		if len(allow) == 0 {
			handler = e.notFound.Handler()
			fallbackStatus = http.StatusNotFound
		} else {
			// Written before the chain runs rather than inside the leaf, so the
			// composition around the leaf stays the same value for every request
			// and can be cached. A layer therefore sees the header already set,
			// which is the right way round: it can read or replace it.
			sort.Strings(allow)
			ctx.SetHeader("Allow", strings.Join(allow, ", "))
			handler = e.notAllowed.Handler()
			fallbackStatus = http.StatusMethodNotAllowed
		}
	}

	err := handler(ctx)
	if err != nil && !ctx.rw.written {
		e.errHandler(ctx, err)
		commitErrorStatus(&ctx.rw, err)
	}
	if err == nil && fallbackStatus != 0 && !ctx.rw.written && ctx.rw.status == http.StatusOK {
		// An engine-scope layer returned without calling next and without
		// answering, so the unmatched-path error never reached the error handler.
		// Nothing runs below this point, and the 200 every response starts at
		// would become the answer for a path no route matched. The fallback's own
		// status is the floor — the same rule commitErrorStatus applies to an
		// error handler that renders nothing — and a status the layer chose for
		// itself still wins.
		ctx.rw.status = fallbackStatus
	}
	if !ctx.rw.written {
		// A handler — or an error handler — that only called Status still
		// owes a response; without this the server would answer 200.
		ctx.rw.WriteHeader(ctx.rw.status)
	}

	// Recycled without a defer: a context whose handler panicked is left to
	// the garbage collector rather than handed to the next request, and the
	// happy path saves the defer.
	ctx.recycle()
	e.pool.Put(ctx)
}

// commitErrorStatus records the error's own status when the configured
// httpx.ErrorHandler rendered nothing for it.
//
// Rendering nothing is a legitimate shape — a handler that only logs, and
// leaves the body to a layer above — but nothing runs below this point, and the
// recorded status is still the 200 every response starts at. Committing that
// answered an unmatched path with 200, which caches and monitoring believe.
// The error's own status is the floor; a status the error handler set for
// itself still wins, and a response it wrote is never touched.
func commitErrorStatus(rw *responseWriter, err error) {
	if rw.written || rw.status != http.StatusOK {
		return
	}
	_, status, _ := httpx.ClassifyError(err)
	rw.status = int(status)
}

// staticLeaf is the innermost handler of an unmatched-path chain: it reports the
// error the adapter would have rendered had no middleware been registered. One
// value per Engine, built in New, which is what lets the composition around it be
// cached.
func staticLeaf(err error) httpx.Handler {
	return func(httpx.Context) error { return err }
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
func (e *Engine) IsRunning() bool { return e.running.Load() }

// Do serves req in-process and returns the buffered response. It implements
// httpx.TestRequester. Unlike the other adapters there is no bridging here:
// this is the same code path a real connection takes.
func (e *Engine) Do(req *http.Request) (*http.Response, error) {
	rr := httptest.NewRecorder()
	e.ServeHTTP(rr, req)
	return rr.Result(), nil
}

// clientIP applies the trusted-proxy policy: without trusted proxies the peer
// address wins and forwarding headers are ignored; with them, X-Forwarded-For
// is walked right to left past trusted hops.
func (e *Engine) clientIP(req *http.Request) string {
	remote := remoteIP(req)
	if len(e.trustedProxies) == 0 || !ipInAny(remote, e.trustedProxies) {
		return remote
	}
	for _, raw := range reverse(strings.Split(req.Header.Get("X-Forwarded-For"), ",")) {
		ip := strings.TrimSpace(raw)
		if ip == "" {
			continue
		}
		if parsed := net.ParseIP(ip); parsed == nil {
			// A malformed entry ends the chain: everything to its left is
			// unverifiable.
			return remote
		}
		if ipInAny(ip, e.trustedProxies) {
			continue
		}
		return ip
	}
	return remote
}

func remoteIP(req *http.Request) string {
	host, _, err := net.SplitHostPort(strings.TrimSpace(req.RemoteAddr))
	if err != nil {
		return strings.TrimSpace(req.RemoteAddr)
	}
	return host
}

func ipInAny(ip string, nets []*net.IPNet) bool {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return false
	}
	for _, n := range nets {
		if n.Contains(parsed) {
			return true
		}
	}
	return false
}

func reverse(list []string) []string {
	out := make([]string, len(list))
	for i, v := range list {
		out[len(list)-1-i] = v
	}
	return out
}
