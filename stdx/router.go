package stdx

import (
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path"
	"slices"
	"strings"

	"github.com/go-sphere/httpx"
)

var (
	_ httpx.Router           = (*Router)(nil)
	_ httpx.InterceptorScope = (*Router)(nil)
)

// anyMethods is what Any registers, matching the set the other adapters expose.
var anyMethods = []string{
	http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch,
	http.MethodDelete, http.MethodHead, http.MethodOptions,
	http.MethodConnect, http.MethodTrace,
}

type Router struct {
	engine   *Engine
	basePath string
	// middlewares are snapshotted into every route registered afterwards, so
	// Use after a registration only affects later routes — the same rule the
	// other adapters inherit from their frameworks.
	middlewares  []httpx.Middleware
	interceptors []httpx.Interceptor
}

func (r *Router) Use(m ...httpx.Middleware) {
	r.middlewares = append(r.middlewares, m...)
}

// UseInterceptor registers composed middleware, implementing
// httpx.InterceptorScope.
//
// The chain is composed into every route registered afterwards, so it needs no
// handler slot and no per-request object. The ordering rule that follows from
// that: interceptors always run inside the middleware registered with Use on
// this scope and its parents, whatever order the registration calls were made
// in. Among themselves, interceptors run in registration order, parent scopes
// first.
func (r *Router) UseInterceptor(m ...httpx.Interceptor) {
	if len(m) == 0 {
		return
	}
	// A fresh slice keeps routes registered earlier bound to the chain they
	// were registered with.
	r.interceptors = append(slices.Clone(r.interceptors), m...)
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
	return &Router{
		engine:       r.engine,
		basePath:     joinPaths(r.basePath, prefix),
		middlewares:  cloneMiddlewares(r.middlewares, m...),
		interceptors: r.interceptors,
	}
}

func (r *Router) Handle(method, path string, h httpx.Handler) {
	if err := httpx.ValidateWildcardPath(path); err != nil {
		panic(err)
	}
	pattern := joinPaths(r.basePath, path)
	// Composed once per route, never per request.
	leaf := httpx.ComposeInterceptors(h, r.interceptors)
	r.engine.root.add(strings.ToUpper(method), pattern, &route{
		pattern: pattern,
		chain:   slices.Clone(r.middlewares),
		handler: leaf,
	})
}

// HandleStd mounts a plain net/http handler, implementing httpx.StdHandlerMounter.
// It goes through Handle so interceptors registered on this scope also wrap it.
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
// ordinary route, so interceptors on this scope wrap static requests too.
func (r *Router) StaticFS(prefix string, fsys fs.FS) {
	pattern := httpx.StaticRoutePattern(prefix)
	leaf := stdLeaf(httpx.StaticFileHandler(joinPaths(r.basePath, prefix), fsys))
	r.Handle(http.MethodGet, pattern, leaf)
	r.Handle(http.MethodHead, pattern, leaf)
}

// stdLeaf serves a plain net/http handler from the adapter's own writer and
// request, so a std handler can sit at the end of a composed interceptor
// chain. Unlike the other adapters this needs no bridging: the handler runs on
// the very objects the server handed us.
func stdLeaf(h http.Handler) httpx.Handler {
	return func(ctx httpx.Context) error {
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

func cloneMiddlewares(middlewares []httpx.Middleware, extra ...httpx.Middleware) []httpx.Middleware {
	out := make([]httpx.Middleware, len(middlewares)+len(extra))
	copy(out, middlewares)
	copy(out[len(middlewares):], extra)
	return out
}

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
