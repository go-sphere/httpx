package hertzx

import (
	"bytes"
	"context"
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

func WithErrorHandler(errHandler ErrorHandler) Option {
	return func(conf *Config) {
		conf.errHandler = errHandler
	}
}

// WithHTTPXErrorHandler installs a framework-neutral error handler. The
// handler receives a real httpx.Context backed by the hertz context, so the
// same error-rendering code can be shared across all adapters.
func WithHTTPXErrorHandler(errHandler httpx.ErrorHandler) Option {
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
// entirely (hertz's default trusts every peer). Invalid entries panic at
// construction time.
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
	clientIP   app.ClientIP
	running    atomic.Bool
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
		errHandler: e.errHandler,
	}
}

func (e *Engine) Start() error {
	e.running.Store(true)
	defer e.running.Store(false)
	return e.engine.Run()
}

func (e *Engine) Stop(ctx context.Context) error {
	err := e.engine.Shutdown(ctx)
	if err == nil {
		e.running.Store(false)
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
