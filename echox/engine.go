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
	// Install the adapter default error handler unless the user configured a
	// custom one on the engine. Comparing against echo's default handler makes
	// echox.New(WithEngine(echo.New())) behave the same as echox.New().
	if conf.engine.HTTPErrorHandler == nil || isEchoDefaultErrorHandler(conf.engine) {
		conf.engine.HTTPErrorHandler = DefaultHTTPErrorHandler
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

// DefaultHTTPErrorHandler renders errors with the standard httpx error body.
// It understands *echo.HTTPError (framework 404/405/... errors) so their
// status codes are preserved instead of being reported as 500.
func DefaultHTTPErrorHandler(err error, c echo.Context) {
	if c.Response().Committed {
		return
	}
	status, body := httpx.RenderError(normalizeEchoError(err))
	_ = c.JSON(status, body)
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

// WithAddr sets the listen address. It is the framework-neutral equivalent of
// WithServerAddr, present on every adapter.
func WithAddr(addr string) Option {
	return WithServerAddr(addr)
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
	running    atomic.Bool
	closed     atomic.Bool
}

func New(opts ...Option) httpx.Engine {
	conf := NewConfig(opts...)
	if conf.ipExtractor != nil {
		conf.engine.IPExtractor = conf.ipExtractor
	}
	conf.server.Handler = conf.engine
	engine := &Engine{
		engine:     conf.engine,
		server:     conf.server,
		errHandler: conf.errHandler,
	}
	engine.running.Store(false)
	return engine
}

func (e *Engine) Use(middleware ...httpx.Middleware) {
	e.engine.Use(adaptMiddlewares(middleware, e.errHandler)...)
}

func (e *Engine) Group(prefix string, m ...httpx.Middleware) httpx.Router {
	return &Router{
		group:      e.engine.Group(prefix, adaptMiddlewares(m, e.errHandler)...),
		basePath:   joinPaths("/", prefix),
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

// Do serves req in-process through the echo engine and returns the buffered
// response. It implements httpx.TestRequester.
func (e *Engine) Do(req *http.Request) (*http.Response, error) {
	rr := httptest.NewRecorder()
	e.engine.ServeHTTP(rr, req)
	return rr.Result(), nil
}
