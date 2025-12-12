package hertzx

import (
	"context"
	"io"
	"net/http"
	"strings"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/adaptor"
	"github.com/cloudwego/hertz/pkg/route"
	"github.com/go-sphere/httpx"
)

var _ httpx.Router = (*router)(nil)

type router struct {
	engine       *server.Hertz
	group        *route.RouterGroup
	middleware   *httpx.MiddlewareChain
	errorHandler httpx.ErrorHandler
}

func (r *router) Use(m ...httpx.Middleware) {
	r.middleware.Use(m...)
}

func (r *router) Group(prefix string, m ...httpx.Middleware) httpx.Router {
	child := &router{
		engine:       r.engine,
		group:        r.group.Group(prefix),
		middleware:   r.middleware.Clone(),
		errorHandler: r.errorHandler,
	}
	child.Use(m...)
	return child
}

func (r *router) Handle(method, path string, h httpx.Handler) {
	method = strings.ToUpper(method)
	r.group.Handle(method, path, r.toHertzHandler(h))
}

func (r *router) Any(path string, h httpx.Handler) {
	r.group.Any(path, r.toHertzHandler(h))
}

func (r *router) Static(prefix, root string) {
	r.group.Static(prefix, root)
}

func (r *router) Mount(path string, h http.Handler) {
	base := strings.TrimSuffix(path, "/")
	if base == "" {
		base = "/"
	}

	handler := r.toHertzHandler(func(ctx httpx.Context) error {
		hc, ok := ctx.(*hertzContext)
		if !ok {
			return nil
		}
		req, err := adaptor.GetCompatRequest(&hc.ctx.Request)
		if err != nil {
			return err
		}
		req = req.WithContext(hc.baseCtx)
		w := adaptor.GetCompatResponseWriter(&hc.ctx.Response)
		h.ServeHTTP(w, req)
		return nil
	})
	r.group.Any(base, handler)
	if base == "/" {
		r.group.Any("/*path", handler)
	} else {
		r.group.Any(base+"/*path", handler)
	}
}

func (r *router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	pool := r.engine.GetCtxPool()
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

	r.engine.ServeHTTP(req.Context(), ctx)

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

func (r *router) toHertzHandler(h httpx.Handler) app.HandlerFunc {
	handler := r.middleware.Then(h)
	return func(ctx context.Context, rc *app.RequestContext) {
		hc := newHertzContext(ctx, rc, r.errorHandler)
		if err := handler(hc); err != nil {
			(r.errorHandler)(hc, err)
		}
	}
}
