package ginx

import (
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path"
	"slices"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/go-sphere/httpx"
)

var (
	_ httpx.Router           = (*Router)(nil)
	_ httpx.InterceptorScope = (*Router)(nil)
)

type Router struct {
	group      *gin.RouterGroup
	errHandler ErrorHandler
	// interceptors are composed into each route at registration instead of
	// occupying a gin handler slot, so they run inside everything registered
	// through Use/UseNative.
	interceptors []httpx.Interceptor
}

func (r *Router) Use(m ...httpx.Middleware) {
	r.group.Use(adaptMiddlewares(m, r.errHandler)...)
}

// UseNative registers native gin middleware on this group. Prefer it over
// wrapping a gin.HandlerFunc with AdaptGinMiddleware: the handler runs with no
// adapter in between.
func (r *Router) UseNative(handlers ...gin.HandlerFunc) {
	r.group.Use(handlers...)
}

// UseInterceptor registers composed middleware, implementing
// httpx.InterceptorScope.
//
// The chain is composed into every route registered afterwards, so it needs no
// gin handler slot and no per-request object. The ordering rule that follows
// from that: interceptors always run inside the middleware registered with Use
// or UseNative on this scope and its parents, whatever order the registration
// calls were made in. Among themselves, interceptors run in registration
// order, parent scopes first.
func (r *Router) UseInterceptor(m ...httpx.Interceptor) {
	if len(m) == 0 {
		return
	}
	// A fresh slice keeps routes registered earlier bound to the chain they
	// were registered with, the same rule gin applies to Use.
	r.interceptors = append(slices.Clone(r.interceptors), m...)
}

func (r *Router) BasePath() string {
	return r.group.BasePath()
}

func (r *Router) SupportsRouterFeature(feature httpx.RouterFeature) bool {
	switch feature {
	case httpx.RouterFeatureNamedWildcard:
		return true
	default:
		return false
	}
}

func (r *Router) Group(prefix string, m ...httpx.Middleware) httpx.Router {
	return &Router{
		group:        r.group.Group(prefix, adaptMiddlewares(m, r.errHandler)...),
		errHandler:   r.errHandler,
		interceptors: r.interceptors,
	}
}

func (r *Router) Handle(method, path string, h httpx.Handler) {
	mustValidWildcard(path)
	r.group.Handle(strings.ToUpper(method), path, r.toGinHandler(h))
}

// HandleStd mounts a plain net/http handler, implementing httpx.StdHandlerMounter.
// It goes through Handle so interceptors registered on this scope also wrap it.
func (r *Router) HandleStd(method, path string, h http.Handler) {
	r.Handle(method, path, stdLeaf(h))
}

// mustValidWildcard fails registration loudly and uniformly across adapters
// for wildcard shapes the shared contract does not support.
func mustValidWildcard(path string) {
	if err := httpx.ValidateWildcardPath(path); err != nil {
		panic(err)
	}
}

func (r *Router) Any(path string, h httpx.Handler) {
	mustValidWildcard(path)
	r.group.Any(path, r.toGinHandler(h))
}

func (r *Router) Static(prefix, root string) {
	r.StaticFS(prefix, os.DirFS(root))
}

// StaticFS serves fsys through httpx.StaticFileHandler and registers it as an
// ordinary route, so interceptors on this scope wrap static requests too.
func (r *Router) StaticFS(prefix string, fsys fs.FS) {
	pattern := httpx.StaticRoutePattern(prefix)
	leaf := stdLeaf(httpx.StaticFileHandler(path.Join(r.group.BasePath(), prefix), fsys))
	r.Handle(http.MethodGet, pattern, leaf)
	r.Handle(http.MethodHead, pattern, leaf)
}

// stdLeaf serves a plain net/http handler through gin's writer and request, so
// a std handler can sit at the end of a composed interceptor chain.
func stdLeaf(h http.Handler) httpx.Handler {
	return func(ctx httpx.Context) error {
		gc, ok := httpx.AsNativeContext[*gin.Context](ctx)
		if !ok {
			return errors.New("ginx: gin context type error")
		}
		h.ServeHTTP(gc.Writer, gc.Request)
		return nil
	}
}

// GET registers a new GET route for a path with matching handler.
func (r *Router) GET(path string, h httpx.Handler) {
	r.Handle("GET", path, h)
}

// POST registers a new POST route for a path with matching handler.
func (r *Router) POST(path string, h httpx.Handler) {
	r.Handle("POST", path, h)
}

// PUT registers a new PUT route for a path with matching handler.
func (r *Router) PUT(path string, h httpx.Handler) {
	r.Handle("PUT", path, h)
}

// DELETE registers a new DELETE route for a path with matching handler.
func (r *Router) DELETE(path string, h httpx.Handler) {
	r.Handle("DELETE", path, h)
}

// PATCH registers a new PATCH route for a path with matching handler.
func (r *Router) PATCH(path string, h httpx.Handler) {
	r.Handle("PATCH", path, h)
}

// HEAD registers a new HEAD route for a path with matching handler.
func (r *Router) HEAD(path string, h httpx.Handler) {
	r.Handle("HEAD", path, h)
}

// OPTIONS registers a new OPTIONS route for a path with matching handler.
func (r *Router) OPTIONS(path string, h httpx.Handler) {
	r.Handle("OPTIONS", path, h)
}

func (r *Router) toGinHandler(h httpx.Handler) gin.HandlerFunc {
	// Composed once per route, never per request.
	h = httpx.ComposeInterceptors(h, r.interceptors)
	return func(gc *gin.Context) {
		ctx := newGinContext(gc)
		if err := h(ctx); err != nil {
			_ = gc.Error(err)
			// Skip the error handler when the response is already committed
			// (or the chain aborted) so a partial response is not corrupted
			// by a second body.
			if !gc.IsAborted() && !gc.Writer.Written() {
				r.errHandler(gc, err)
			}
			if !gc.IsAborted() {
				gc.Abort()
			}
		}
	}
}
