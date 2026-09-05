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
