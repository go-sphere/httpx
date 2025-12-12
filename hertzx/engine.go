package hertzx

import (
	"io"
	"net/http"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/adaptor"
	"github.com/go-sphere/httpx"
)

var _ httpx.Engine = (*Engine)(nil)

type Engine struct {
	engine       *server.Hertz
	middleware   *httpx.MiddlewareChain
	errorHandler httpx.ErrorHandler
}

func New(opts ...httpx.Option[*server.Hertz]) *Engine {
	conf := httpx.NewConfig(opts...)
	if conf.Engine == nil {
		conf.Engine = server.Default()
	}

	middleware := httpx.NewMiddlewareChain()
	middleware.Use(conf.Middleware.Middlewares()...)
	return &Engine{
		engine:       conf.Engine,
		middleware:   middleware,
		errorHandler: conf.ErrorHandler,
	}
}

func (e *Engine) Use(middleware ...httpx.Middleware) {
	e.middleware.Use(middleware...)
}

func (e *Engine) Group(prefix string, m ...httpx.Middleware) httpx.Router {
	middleware := e.middleware.Clone()
	middleware.Use(m...)
	return &Router{
		group:        e.engine.Group(prefix),
		middleware:   middleware,
		errorHandler: e.errorHandler,
	}
}

func (e *Engine) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	pool := e.engine.GetCtxPool()
	ctx := pool.Get().(*app.RequestContext)
	ctx.ResetWithoutConn()
	defer func() {
		ctx.ResetWithoutConn()
		pool.Put(ctx)
	}()

	if err := adaptor.CopyToHertzRequest(req, &ctx.Request); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	e.engine.ServeHTTP(req.Context(), ctx)

	resp := &ctx.Response
	resp.Header.VisitAll(func(k, v []byte) {
		w.Header().Add(string(k), string(v))
	})

	status := resp.StatusCode()
	if status == 0 {
		status = http.StatusOK
	}
	w.WriteHeader(status)

	if resp.MustSkipBody() {
		_ = resp.CloseBodyStream()
		return
	}

	if resp.IsBodyStream() {
		_, _ = io.Copy(w, resp.BodyStream())
		_ = resp.CloseBodyStream()
		return
	}

	if body := resp.Body(); len(body) > 0 {
		_, _ = w.Write(body)
	}
}
