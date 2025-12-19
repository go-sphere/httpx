package fasthttpx

import (
	"context"
	"net"
	"strings"

	"github.com/go-sphere/httpx"
	"github.com/valyala/fasthttp"
)

var _ httpx.Engine = (*Engine)(nil)

type routeEntry struct {
	method      string
	path        string
	handler     httpx.Handler
	middlewares []httpx.Middleware
}

type Engine struct {
	server         *fasthttp.Server
	middlewares    []httpx.Middleware
	routes         []routeEntry
	staticHandlers map[string]fasthttp.RequestHandler
	addr           string
	listener       net.Listener
}

func New() *Engine {
	e := &Engine{
		middlewares:    make([]httpx.Middleware, 0),
		routes:         make([]routeEntry, 0),
		staticHandlers: make(map[string]fasthttp.RequestHandler),
		addr:           ":8080",
	}
	e.server = &fasthttp.Server{
		Handler: e.defaultHandler,
	}
	return e
}

func (e *Engine) Use(m ...httpx.Middleware) {
	e.middlewares = append(e.middlewares, m...)
}

func (e *Engine) Group(prefix string, m ...httpx.Middleware) httpx.Router {
	return &Router{
		basePath:    prefix,
		routes:      make([]route, 0),
		middlewares: cloneMiddlewares(e.middlewares, m...),
		engine:      e,
	}
}

func (e *Engine) Start() error {
	listener, err := net.Listen("tcp", e.addr)
	if err != nil {
		return err
	}
	e.listener = listener
	e.addr = listener.Addr().String()
	
	// Serve blocks until the server is shut down
	err = e.server.Serve(listener)
	if err != nil {
		e.listener = nil
	}
	return err
}

func (e *Engine) Stop(ctx context.Context) error {
	if e.listener != nil {
		err := e.listener.Close()
		e.listener = nil
		return err
	}
	return nil
}

func (e *Engine) IsRunning() bool {
	return e.listener != nil
}

func (e *Engine) Addr() string {
	if e.listener != nil {
		return e.listener.Addr().String()
	}
	return e.addr
}

func (e *Engine) SetAddr(addr string) {
	if !strings.Contains(addr, ":") {
		addr = ":" + addr
	}
	e.addr = addr
}

func (e *Engine) addRoute(method, path string, handler httpx.Handler, middlewares []httpx.Middleware) {
	e.routes = append(e.routes, routeEntry{
		method:      method,
		path:        path,
		handler:     handler,
		middlewares: middlewares,
	})
}

func (e *Engine) addStaticHandler(prefix string, handler fasthttp.RequestHandler) {
	e.staticHandlers[prefix] = handler
}

func (e *Engine) defaultHandler(ctx *fasthttp.RequestCtx) {
	method := string(ctx.Method())
	path := string(ctx.Path())
	
	// Check static handlers first
	for prefix, handler := range e.staticHandlers {
		if strings.HasPrefix(path, prefix) {
			handler(ctx)
			return
		}
	}
	
	// Check routes
	for _, route := range e.routes {
		if route.method == method && route.path == path {
			// Create context with combined middlewares
			allMiddlewares := append(e.middlewares, route.middlewares...)
			
			fc := newFastHTTPContext(ctx, allMiddlewares)
			fc.handler = route.handler
			fc.Next()
			return
		}
	}
	
	// Not found
	ctx.SetStatusCode(fasthttp.StatusNotFound)
}