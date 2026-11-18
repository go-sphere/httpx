package stdx

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-sphere/httpx"
)

var (
	_ httpx.Engine = (*Engine)(nil)
	_ http.Handler = (*Engine)(nil)
)

// Config is the resolved configuration [New] builds an [Engine] from. Its
// fields are unexported; set them with [Option] values.
type Config struct {
	server         *http.Server
	errHandler     httpx.ErrorHandler
	trustedProxies []*net.IPNet
	maxBodySize    int64
}

// Option configures an [Engine] at construction. Options are applied in
// order, so a later option overrides an earlier one touching the same setting.
type Option func(*Config)

// NewConfig applies opts in order and fills the defaults: an http.Server
// listening on ":8080" (see [WithAddr] for its timeouts) and
// [DefaultErrorHandler]. [New] calls it; most callers pass options to New
// directly.
func NewConfig(opts ...Option) *Config {
	conf := &Config{}
	for _, opt := range opts {
		opt(conf)
	}
	if conf.server == nil {
		conf.server = newServer(":8080")
	}
	if conf.errHandler == nil {
		conf.errHandler = DefaultErrorHandler
	}
	return conf
}

// DefaultErrorHandler renders errors with the standard httpx error body
// (httpx.RenderError) as JSON. It is installed when [WithErrorHandler] is not
// used. On stdx the native error-handler shape is httpx.ErrorHandler itself,
// so this is interchangeable with httpx.DefaultErrorHandler.
func DefaultErrorHandler(ctx httpx.Context, err error) {
	status, body := httpx.RenderError(err)
	if c, ok := ctx.(*stdContext); ok {
		c.scratch = appendErrorBody(c.scratch[:0], body)
		_ = c.writeJSONBytes(status, c.scratch)
		return
	}
	_ = ctx.JSON(status, body)
}

// WithServer supplies the http.Server to serve with. [New] installs the
// engine as its Handler, replacing any handler already set; the server's
// other fields (timeouts, Addr) are kept, none of the defaults [WithAddr]
// describes is applied. Start serves plain HTTP on a
// listener it binds itself, so the server's TLS configuration is not used.
//
// Order matters with [WithAddr]: WithAddr after WithServer sets Addr on this
// server, while WithServer after WithAddr replaces the server WithAddr made.
func WithServer(server *http.Server) Option {
	return func(conf *Config) { conf.server = server }
}

// WithAddr sets the listen address, creating the http.Server when none was
// supplied. It is the framework-neutral option present on every adapter. The
// default is ":8080"; an empty address listens on ":http".
//
// A server the adapter creates limits how long a client may take to send its
// request headers ([DefaultReadHeaderTimeout]) and how long an idle keep-alive
// connection stays open ([DefaultIdleTimeout]), so slow or idle clients cannot
// hold connections forever. It sets no read or write timeout for the whole
// request, which would cut uploads and streamed responses; supply a server
// with [WithServer] to choose other limits.
func WithAddr(addr string) Option {
	return func(conf *Config) {
		if conf.server == nil {
			conf.server = newServer(addr)
		} else {
			conf.server.Addr = addr
		}
	}
}

// The timeouts of a server the adapter creates; see [WithAddr].
const (
	DefaultReadHeaderTimeout = 10 * time.Second
	DefaultIdleTimeout       = 120 * time.Second
)

func newServer(addr string) *http.Server {
	return &http.Server{
		Addr:              addr,
		ReadHeaderTimeout: DefaultReadHeaderTimeout,
		IdleTimeout:       DefaultIdleTimeout,
	}
}

// WithErrorHandler installs a framework-neutral error handler. Errors returned
// by httpx handlers and middleware, and the 404/405 answers for unmatched
// requests, are rendered through it unless the response is already committed.
// If the handler renders nothing, the error's own status is still sent. A nil
// errHandler is ignored: the handler configured before it, or
// [DefaultErrorHandler], stays in place.
func WithErrorHandler(errHandler httpx.ErrorHandler) Option {
	return func(conf *Config) {
		if errHandler != nil {
			conf.errHandler = errHandler
		}
	}
}

// WithMaxBodySize limits request bodies on every route to n bytes; n <= 0,
// the default, means no limit. A request declaring a larger Content-Length is
// refused with a *http.MaxBytesError before the route's middleware runs, which
// the configured error handler renders as 413. A body of unknown length is cut
// at n bytes: the read that passes the limit fails with a *http.MaxBytesError,
// which Bind* report as 413 and a handler returning it is rendered as 413.
func WithMaxBodySize(n int64) Option {
	return func(conf *Config) { conf.maxBodySize = n }
}

// WithTrustedProxies sets the uniform trusted-proxy policy for ClientIP:
// X-Forwarded-For is honored only when the direct peer is inside the given
// IPs/CIDRs, and an empty list ignores forwarding headers entirely (which is
// also the default). When every hop is trusted the leftmost entry is returned,
// matching gin. Blank entries are skipped, an entry that is not a bare IP
// (brackets, a port) ends the chain at the peer, and X-Real-IP is never read.
// Invalid entries panic at construction time.
func WithTrustedProxies(proxies ...string) Option {
	return func(conf *Config) {
		cidrs, err := httpx.ParseCIDRs(proxies)
		if err != nil {
			panic(err)
		}
		conf.trustedProxies = cidrs
	}
}

// Engine is the adapter's http.Handler: it owns the route tree, the engine
// middleware chain and the http.Server. Nothing wraps net/http here, so an
// Engine can also be mounted inside another net/http server.
//
// Construct it with [New]; the zero value is not usable. Register routes and
// middleware before serving; ServeHTTP is safe for concurrent use. An Engine is
// single-use: once stopped, Start returns httpx.ErrEngineClosed, although
// ServeHTTP keeps serving when the engine is mounted elsewhere.
type Engine struct {
	root   *node
	server *http.Server

	errHandler     httpx.ErrorHandler
	trustedProxies []*net.IPNet
	maxBodySize    int64

	// chain is the engine scope, referenced by every group created from this
	// engine; see Router.Use. notFound and notAllowed carry it into the
	// unmatched-path answers, which is the one place a group's chain must not
	// reach.
	chain      *httpx.MiddlewareChain
	notFound   *httpx.MiddlewareFallback
	notAllowed *httpx.MiddlewareFallback

	pool        sync.Pool
	lifecycleMu sync.Mutex
	ln          net.Listener
	started     atomic.Bool
	running     atomic.Bool
	closed      atomic.Bool
}

// New returns a net/http-backed engine configured by opts. The dynamic type
// is *[Engine], which also implements http.Handler and httpx.TestRequester.
// New does not listen; call Start, or serve the engine as an http.Handler.
func New(opts ...Option) httpx.Engine {
	conf := NewConfig(opts...)
	engine := &Engine{
		root:           &node{},
		server:         conf.server,
		errHandler:     conf.errHandler,
		trustedProxies: conf.trustedProxies,
		maxBodySize:    conf.maxBodySize,
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

// Group returns a [Router] for prefix with m registered on it as if by Use;
// "" and "/" both mean the root. The router sees middleware later registered
// on the engine for the routes it registers afterwards.
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
	path := req.URL.Path
	if path == "" {
		path = "/"
	}
	r, allow := e.root.match(req.Method, path, &ctx.values)
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
			slices.Sort(allow)
			ctx.SetHeader("Allow", strings.Join(allow, ", "))
			handler = e.notAllowed.Handler()
			fallbackStatus = http.StatusMethodNotAllowed
		}
	}

	defer func() {
		if ctx != nil {
			removeReplacedMultipartForm(req, ctx.req)
		}
	}()
	var err error
	if r != nil {
		err = httpx.LimitRequestBody(w, req, e.maxBodySize)
	}
	if err == nil {
		err = handler(ctx)
	}
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
	removeReplacedMultipartForm(req, ctx.req)

	// Recycled without a defer: a context whose handler panicked is left to
	// the garbage collector rather than handed to the next request, and the
	// happy path saves the defer. Clear ctx before Put so the panic-cleanup
	// defer does not read the pooled instance concurrently.
	ctx.recycle()
	recycled := ctx
	ctx = nil
	e.pool.Put(recycled)
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

// removeReplacedMultipartForm deletes the temporary files of a multipart form
// parsed on cur, a request that replaced orig during the chain (SetContext, a
// net/http middleware). net/http removes them only for the request it served,
// so without this an upload spilled to disk would stay there.
func removeReplacedMultipartForm(orig, cur *http.Request) {
	if cur != nil && orig != nil && cur != orig && cur.MultipartForm != nil && cur.MultipartForm != orig.MultipartForm {
		_ = cur.MultipartForm.RemoveAll()
	}
}

// staticLeaf is the innermost handler of an unmatched-path chain: it reports the
// error the adapter would have rendered had no middleware been registered. One
// value per Engine, built in New, which is what lets the composition around it be
// cached.
func staticLeaf(err error) httpx.Handler {
	return func(httpx.Context) error { return err }
}

var errAlreadyStarted = errors.New("stdx: engine already started")

// Start binds the server's address (":http" when empty), then serves until
// [Engine.Stop]. IsRunning becomes true only after the listener is bound. It
// returns nil after Stop, httpx.ErrEngineClosed when Stop was already called,
// an error when Start was already called, and the listen or serve error
// otherwise.
func (e *Engine) Start() error {
	e.lifecycleMu.Lock()
	if e.closed.Load() {
		e.lifecycleMu.Unlock()
		return httpx.ErrEngineClosed
	}
	if !e.started.CompareAndSwap(false, true) {
		e.lifecycleMu.Unlock()
		return errAlreadyStarted
	}
	addr := e.server.Addr
	if addr == "" {
		addr = ":http"
	}
	// Bind under lifecycleMu so a concurrent Stop sets closed/inShutdown either
	// strictly before Start checks closed or after ln exists for Serve.
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		e.lifecycleMu.Unlock()
		return err
	}
	e.ln = ln
	e.running.Store(true)
	e.lifecycleMu.Unlock()
	defer func() {
		e.lifecycleMu.Lock()
		e.ln = nil
		e.lifecycleMu.Unlock()
		e.running.Store(false)
	}()
	if err := e.server.Serve(ln); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// Stop marks the engine closed and shuts the server down with httpx.Close:
// in-flight requests drain until ctx is done, after which the server is
// force-closed, cutting the connections still being served, and nil is
// returned. A nil ctx waits indefinitely. Stop on an engine that never
// started returns nil, and Start then returns httpx.ErrEngineClosed.
func (e *Engine) Stop(ctx context.Context) error {
	e.lifecycleMu.Lock()
	e.closed.Store(true)
	ln := e.ln
	e.lifecycleMu.Unlock()
	// httpx.Close force-closes when the graceful drain fails, so the server is
	// down whatever it returns and the running flag falls with it.
	defer e.running.Store(false)
	err := httpx.Close(ctx, e.server)
	if ln != nil {
		if closeErr := ln.Close(); closeErr != nil && !errors.Is(closeErr, net.ErrClosed) {
			err = errors.Join(err, closeErr)
		}
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
// is walked right to left past trusted hops. When every hop is trusted the
// leftmost entry is returned — the address the first trusted hop saw, which is
// also what gin resolves. Falling back to the peer there would name the
// nearest proxy instead (127.0.0.1 for a same-host reverse proxy), so a client
// that is itself inside a trusted range would be reported as the proxy.
func (e *Engine) clientIP(req *http.Request) string {
	remote := remoteIP(req)
	if len(e.trustedProxies) == 0 || !ipInAny(remote, e.trustedProxies) {
		return remote
	}
	// Walk every X-Forwarded-For header line from right to left so multiple
	// X-Forwarded-For headers cannot hide earlier hops behind req.Header.Get().
	// Splitting and reversing slices would allocate twice per call, whereas
	// walking backwards in place allocates nothing.
	xffs := req.Header["X-Forwarded-For"]
	if len(xffs) == 0 {
		return remote
	}
	for idx := len(xffs) - 1; idx >= 0; idx-- {
		for rest := xffs[idx]; rest != ""; {
			raw := rest
			if i := strings.LastIndexByte(rest, ','); i >= 0 {
				raw, rest = rest[i+1:], rest[:i]
			} else {
				rest = ""
			}
			ip := strings.TrimSpace(raw)
			if ip == "" {
				continue
			}
			// Parsed once and handed to the range check.
			parsed := net.ParseIP(ip)
			if parsed == nil {
				// A malformed entry ends the chain: everything to its left is
				// unverifiable.
				return remote
			}
			if netsContain(e.trustedProxies, parsed) {
				continue
			}
			return ip
		}
	}
	// Every hop, the client included, is inside the trusted ranges, so the
	// leftmost entry is that client. Everything non-blank on the way here parsed
	// as an IP; the one chain with nothing verifiable left is the one whose left
	// edge is blank, and the peer still wins there.
	first := xffs[0]
	if i := strings.IndexByte(first, ','); i >= 0 {
		first = first[:i]
	}
	if first = strings.TrimSpace(first); first != "" {
		return first
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
	return netsContain(nets, parsed)
}

func netsContain(nets []*net.IPNet, ip net.IP) bool {
	for _, n := range nets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}
