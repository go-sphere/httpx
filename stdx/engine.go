package stdx

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/go-sphere/httpx"
)

var (
	_ httpx.Engine           = (*Engine)(nil)
	_ httpx.InterceptorScope = (*Engine)(nil)
	_ http.Handler           = (*Engine)(nil)
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

	// middlewares are the engine-wide layers: inherited by groups created
	// afterwards, and the only ones that also run for unmatched paths.
	middlewares  []httpx.Middleware
	interceptors []httpx.Interceptor

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
	}
	engine.pool.New = func() any {
		ctx := &stdContext{engine: engine}
		ctx.native.c = ctx
		return ctx
	}
	engine.server.Handler = engine
	engine.running.Store(false)
	return engine
}

func (e *Engine) Use(middleware ...httpx.Middleware) {
	e.middlewares = append(e.middlewares, middleware...)
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
		engine:       e,
		basePath:     joinPaths("/", prefix),
		middlewares:  cloneMiddlewares(e.middlewares, m...),
		interceptors: e.interceptors,
	}
}

// ServeHTTP dispatches one request: resolve the route, run its chain, and
// render an error only when the handlers produced no response of their own.
func (e *Engine) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	ctx, _ := e.pool.Get().(*stdContext)
	ctx.reset(w, req)

	r, allow := e.root.match(req.Method, req.URL.Path, &ctx.values)
	if r != nil {
		ctx.route = r
		ctx.chain = r.chain
		ctx.leaf = r.handler
	} else {
		// Unmatched paths are outside every group, so only the engine's own
		// middleware runs — the same rule the other adapters inherit.
		ctx.chain = e.middlewares
		ctx.leaf = notAllowedLeaf(allow)
	}

	err := ctx.Next()
	if err != nil && !ctx.rw.written {
		e.errHandler(ctx, err)
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

// notAllowedLeaf answers a path that no route matched: 404, or 405 with an
// Allow header when the path exists under other methods.
func notAllowedLeaf(allow []string) httpx.Handler {
	if len(allow) == 0 {
		return func(ctx httpx.Context) error {
			return httpx.NewNotFoundError(http.StatusText(http.StatusNotFound))
		}
	}
	sort.Strings(allow)
	header := strings.Join(allow, ", ")
	return func(ctx httpx.Context) error {
		ctx.SetHeader("Allow", header)
		return httpx.NewError(http.StatusMethodNotAllowed, 0,
			http.StatusText(http.StatusMethodNotAllowed), nil)
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
	err := httpx.Close(ctx, e.server)
	if err == nil {
		e.running.Store(false)
	}
	return err
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
