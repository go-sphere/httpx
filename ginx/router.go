package ginx

import (
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/go-sphere/httpx"
)

var _ httpx.Router = (*Router)(nil)

type Router struct {
	group      *gin.RouterGroup
	errHandler ErrorHandler
	// chain is composed into each route at registration instead of occupying a
	// gin handler slot, so httpx middleware runs inside everything registered
	// through UseNative. It references the engine's chain rather than copying
	// it; see httpx.MiddlewareChain.
	chain *httpx.MiddlewareChain
}

// commitErrorStatus records the error's own status when the configured
// ErrorHandler rendered nothing for it.
//
// Rendering nothing is a legitimate shape — a handler that only logs, and
// leaves the body to a layer above — but the chain is aborted right after, and
// the status gin has recorded is still the 200 every response starts at, so the
// request answered 200 with an empty body. The error's own status is the floor;
// a status the error handler set for itself still wins, and a response it wrote
// is never touched (the caller checks Written first). On gin's own
// NoRoute/NoMethod path nothing changes, because gin records 404/405 before
// running the fallback and the guard below already holds.
func commitErrorStatus(gc *gin.Context, err error) {
	if gc.Writer.Written() || gc.Writer.Status() != http.StatusOK {
		return
	}
	_, status, _ := httpx.ClassifyError(err)
	gc.Status(int(status))
}

// Use registers httpx middleware on this scope, implementing
// httpx.MiddlewareScope. The chain is composed into every route registered
// afterwards, so it needs no gin handler slot and no per-request object; see
// httpx.Middleware and httpx.MiddlewareChain for the ordering rules.
func (r *Router) Use(m ...httpx.Middleware) {
	r.chain.Use(m...)
}

// UseNative registers native gin middleware on this group, the only way to
// mount a gin.HandlerFunc: it gets its own gin handler slot, which an
// httpx.Middleware wrapper could not reproduce — all httpx layers share the
// route's single slot, so the wrapped handler's c.Next() would advance gin past
// that slot rather than into the httpx chain.
func (r *Router) UseNative(handlers ...gin.HandlerFunc) {
	r.group.Use(handlers...)
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
	sub := r.chain.Sub()
	sub.Use(m...)
	return &Router{
		group:      r.group.Group(prefix),
		errHandler: r.errHandler,
		chain:      sub,
	}
}

func (r *Router) Handle(method, path string, h httpx.Handler) {
	mustValidWildcard(path)
	r.group.Handle(strings.ToUpper(method), path, r.toGinHandler(h))
}

// HandleStd mounts a plain net/http handler, implementing httpx.StdHandlerMounter.
// It goes through Handle so middleware registered on this scope also wraps it.
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
// ordinary route, so middleware on this scope wraps static requests too.
func (r *Router) StaticFS(prefix string, fsys fs.FS) {
	pattern := httpx.StaticRoutePattern(prefix)
	leaf := stdLeaf(httpx.StaticFileHandler(path.Join(r.group.BasePath(), prefix), fsys))
	r.Handle(http.MethodGet, pattern, leaf)
	r.Handle(http.MethodHead, pattern, leaf)
}

// stdLeaf serves a plain net/http handler through gin's writer and request, so
// a std handler can sit at the end of a composed middleware chain.
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
	h = r.chain.Compose(h)
	return func(gc *gin.Context) {
		ctx := newGinContext(gc)
		if err := h(ctx); err != nil {
			_ = gc.Error(err)
			// Skip the error handler when the response is already committed
			// (or the chain aborted) so a partial response is not corrupted
			// by a second body.
			if !gc.IsAborted() && !gc.Writer.Written() {
				r.errHandler(gc, err)
				commitErrorStatus(gc, err)
			}
			if !gc.IsAborted() {
				gc.Abort()
			}
		}
	}
}
