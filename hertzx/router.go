package hertzx

import (
	"context"
	"io/fs"
	"net/http"
	"path"
	"strings"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/adaptor"
	"github.com/cloudwego/hertz/pkg/route"
	"github.com/go-sphere/httpx"
)

var _ httpx.Router = (*Router)(nil)

type Router struct {
	group        *route.RouterGroup
	middleware   *httpx.MiddlewareChain
	errorHandler httpx.ErrorHandler
}

func (r *Router) Use(m ...httpx.Middleware) {
	r.middleware.Use(m...)
}

func (r *Router) Group(prefix string, m ...httpx.Middleware) httpx.Router {
	child := &Router{
		group:        r.group.Group(prefix),
		middleware:   r.middleware.Clone(),
		errorHandler: r.errorHandler,
	}
	child.Use(m...)
	return child
}

func (r *Router) Handle(method, path string, h httpx.Handler) {
	method = strings.ToUpper(method)
	r.group.Handle(method, path, r.toHertzHandler(h))
}

func (r *Router) Any(path string, h httpx.Handler) {
	r.group.Any(path, r.toHertzHandler(h))
}

func (r *Router) Static(prefix, root string) {
	r.group.Static(prefix, root)
}

func (r *Router) StaticFS(prefix string, fs fs.FS) {
	if strings.Contains(prefix, ":") || strings.Contains(prefix, "*") {
		panic("URL parameters can not be used when serving a static folder")
	}
	absolutePath := path.Join(r.group.BasePath(), prefix)
	fileServer := http.StripPrefix(absolutePath, http.FileServer(http.FS(fs)))
	handler := adaptor.HertzHandler(fileServer)
	urlPattern := path.Join(prefix, "/*filepath")
	r.group.GET(urlPattern, handler)
	r.group.HEAD(urlPattern, handler)
}

func (r *Router) toHertzHandler(h httpx.Handler) app.HandlerFunc {
	handler := r.middleware.Then(h)
	return func(ctx context.Context, rc *app.RequestContext) {
		hc := newHertzContext(ctx, rc, r.errorHandler)
		if err := handler(hc); err != nil {
			(r.errorHandler)(hc, err)
		}
	}
}
