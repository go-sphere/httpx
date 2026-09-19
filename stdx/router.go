package stdx

import (
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path"
	"strings"

	"github.com/go-sphere/httpx"
)

var _ httpx.Router = (*Router)(nil)

// anyMethods is what Any registers, matching the set the other adapters expose.
var anyMethods = []string{
	http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch,
	http.MethodDelete, http.MethodHead, http.MethodOptions,
	http.MethodConnect, http.MethodTrace,
}

type Router struct {
	engine   *Engine
	basePath string
	// chain references the engine's chain rather than copying it; see
	// httpx.MiddlewareChain.
	chain *httpx.MiddlewareChain
}

// Use registers httpx middleware on this scope, implementing
// httpx.MiddlewareScope. The chain is composed into every route registered
// afterwards, so it needs no handler slot and no per-request object; see
// httpx.Middleware and httpx.MiddlewareChain for the ordering rules.
func (r *Router) Use(m ...httpx.Middleware) {
	r.chain.Use(m...)
}

func (r *Router) BasePath() string {
	if r.basePath == "" {
		return "/"
	}
	return r.basePath
}

// SupportsRouterFeature reports the named wildcard: this router matches
// /files/*filepath directly, so httpx.FixWildcardPathIfNeed leaves it alone.
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
		engine:   r.engine,
		basePath: joinPaths(r.basePath, prefix),
		chain:    sub,
	}
}

func (r *Router) Handle(method, path string, h httpx.Handler) {
	if err := httpx.ValidateWildcardPath(path); err != nil {
		panic(err)
	}
	pattern := joinPaths(r.basePath, path)
	r.engine.root.add(strings.ToUpper(method), pattern, &route{
		pattern: pattern,
		// Composed once per route, never per request.
		handler: r.chain.Compose(h),
	})
}

// HandleStd mounts a plain net/http handler, implementing httpx.StdHandlerMounter.
// It goes through Handle so middleware registered on this scope also wraps it.
func (r *Router) HandleStd(method, path string, h http.Handler) {
	r.Handle(method, path, stdLeaf(h))
}

func (r *Router) Any(path string, h httpx.Handler) {
	for _, method := range anyMethods {
		r.Handle(method, path, h)
	}
}

func (r *Router) Static(prefix, root string) {
	r.StaticFS(prefix, os.DirFS(root))
}

// StaticFS serves fsys through httpx.StaticFileHandler and registers it as an
// ordinary route, so middleware on this scope wraps static requests too.
func (r *Router) StaticFS(prefix string, fsys fs.FS) {
	pattern := httpx.StaticRoutePattern(prefix)
	leaf := stdLeaf(httpx.StaticFileHandler(joinPaths(r.basePath, prefix), fsys))
	r.Handle(http.MethodGet, pattern, leaf)
	r.Handle(http.MethodHead, pattern, leaf)
}

// stdLeaf serves a plain net/http handler from the adapter's own writer and
// request, so a std handler can sit at the end of a composed middleware
// chain. Unlike the other adapters this needs no bridging: the handler runs on
// the very objects the server handed us.
func stdLeaf(h http.Handler) httpx.Handler {
	return func(ctx httpx.Context) error {
		if c, ok := ctx.(*stdContext); ok {
			h.ServeHTTP(&c.rw, c.req)
			return nil
		}
		// A wrapped context still reaches the writer through the native hook.
		native, ok := httpx.AsNativeContext[*Native](ctx)
		if !ok {
			return errors.New("stdx: native context type error")
		}
		w, req := native.Unwrap()
		h.ServeHTTP(w, req)
		return nil
	}
}

// GET registers a new GET route for a path with matching handler.
func (r *Router) GET(path string, h httpx.Handler) { r.Handle(http.MethodGet, path, h) }

// POST registers a new POST route for a path with matching handler.
func (r *Router) POST(path string, h httpx.Handler) { r.Handle(http.MethodPost, path, h) }

// PUT registers a new PUT route for a path with matching handler.
func (r *Router) PUT(path string, h httpx.Handler) { r.Handle(http.MethodPut, path, h) }

// DELETE registers a new DELETE route for a path with matching handler.
func (r *Router) DELETE(path string, h httpx.Handler) { r.Handle(http.MethodDelete, path, h) }

// PATCH registers a new PATCH route for a path with matching handler.
func (r *Router) PATCH(path string, h httpx.Handler) { r.Handle(http.MethodPatch, path, h) }

// HEAD registers a new HEAD route for a path with matching handler.
func (r *Router) HEAD(path string, h httpx.Handler) { r.Handle(http.MethodHead, path, h) }

// OPTIONS registers a new OPTIONS route for a path with matching handler.
func (r *Router) OPTIONS(path string, h httpx.Handler) { r.Handle(http.MethodOptions, path, h) }

func joinPaths(absolutePath, relativePath string) string {
	if relativePath == "" {
		return absolutePath
	}
	finalPath := path.Join(absolutePath, relativePath)
	if lastCharIs('/', relativePath) && !lastCharIs('/', finalPath) {
		return finalPath + "/"
	}
	return finalPath
}

func lastCharIs(char uint8, str string) bool {
	if str == "" {
		return false
	}
	return str[len(str)-1] == char
}
