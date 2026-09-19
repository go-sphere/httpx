package echox

import (
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path"
	"slices"
	"strings"
	"sync"

	"github.com/go-sphere/httpx"
	"github.com/labstack/echo/v4"
)

var (
	_ httpx.Router           = (*Router)(nil)
	_ httpx.InterceptorScope = (*Router)(nil)
)

// wildcardRoute remembers what a route looked like before
// FixWildcardPathIfNeed rewrote its named wildcard to the anonymous form echo
// supports: the original parameter name, so Param(name) keeps resolving, and
// the pattern as the caller registered it, so FullPath reports that rather than
// this adapter's normalization.
type wildcardRoute struct {
	param   string
	pattern string
}

// wildcardTable maps a normalized route pattern (with the anonymous "*"
// wildcard) to what it was written as.
//
// One table per engine, never one per process. Normalization is lossy — every
// named wildcard in a scope collapses onto the same pattern, so /files/*filepath
// and /files/*path both become /files/* — and a process-wide table would let the
// engine that registered last own the entry for every engine that shares the
// normalized form. Running several engines in one process is ordinary (a public
// and an internal listener, say), and the symptom is silent: FullPath reports
// another engine's pattern, which is what downstream auth and rate limiting
// match on. The table is created by New and handed to every Router made from it,
// so a Group shares its engine's table and two engines share nothing.
type wildcardTable struct {
	routes sync.Map // normalized pattern -> wildcardRoute
}

func (t *wildcardTable) store(pattern string, route wildcardRoute) {
	t.routes.Store(pattern, route)
}

// lookup resolves a matched route pattern back to its registered form. The
// caller checks the pattern ends in "*" first, so a route without a wildcard
// never pays the map lookup. A nil table (a context built by FromEcho, which
// has no engine behind it) resolves nothing.
func (t *wildcardTable) lookup(pattern string) (wildcardRoute, bool) {
	if t == nil {
		return wildcardRoute{}, false
	}
	v, ok := t.routes.Load(pattern)
	if !ok {
		return wildcardRoute{}, false
	}
	route, ok := v.(wildcardRoute)
	return route, ok
}

type Router struct {
	group        *echo.Group
	basePath     string
	errHandler   httpx.ErrorHandler
	interceptors []httpx.Interceptor
	// wildcards is the engine's table, shared with every Group made from it.
	wildcards *wildcardTable
}

// UseInterceptor registers composed middleware, implementing
// httpx.InterceptorScope.
//
// The chain is composed into every route registered afterwards, so it needs no
// native handler slot and no per-request object. The ordering rule that follows
// from that: interceptors always run inside the middleware registered with Use
// on this scope and its parents, whatever order the registration calls were
// made in. Among themselves, interceptors run in registration order, parent
// scopes first.
func (r *Router) UseInterceptor(m ...httpx.Interceptor) {
	if len(m) == 0 {
		return
	}
	// A fresh slice keeps routes registered earlier bound to the chain they
	// were registered with.
	r.interceptors = append(slices.Clone(r.interceptors), m...)
}

func (r *Router) Use(m ...httpx.Middleware) {
	r.group.Use(adaptMiddlewares(m, r.errHandler, r.wildcards)...)
}

// UseNative registers native echo middleware on this group. Prefer it over
// wrapping an echo.MiddlewareFunc with AdaptEchoMiddleware: the handler runs
// with no adapter in between.
func (r *Router) UseNative(middleware ...echo.MiddlewareFunc) {
	r.group.Use(middleware...)
}

func (r *Router) BasePath() string {
	return r.basePath
}

func (r *Router) SupportsRouterFeature(feature httpx.RouterFeature) bool {
	switch feature {
	case httpx.RouterFeatureNamedWildcard:
		return false
	default:
		return false
	}
}

func (r *Router) Group(prefix string, m ...httpx.Middleware) httpx.Router {
	return &Router{
		group:        r.group.Group(prefix, adaptMiddlewares(m, r.errHandler, r.wildcards)...),
		basePath:     joinPaths(r.basePath, prefix),
		errHandler:   r.errHandler,
		interceptors: r.interceptors,
		wildcards:    r.wildcards,
	}
}

// normalizeWildcardPath rewrites named wildcards (/*filepath) to echo's
// anonymous form (/*) and records the original name so Param("filepath")
// still resolves. Unsupported wildcard shapes panic at registration,
// matching gin/hertz native behavior.
func (r *Router) normalizeWildcardPath(path string) string {
	if err := httpx.ValidateWildcardPath(path); err != nil {
		panic(err)
	}
	orig := httpx.WildcardParamName(path)
	if orig == "" || orig == "*" {
		return path
	}
	fixed, _ := httpx.FixWildcardPathIfNeed(r, path)
	r.wildcards.store(joinPaths(r.basePath, fixed), wildcardRoute{
		param:   orig,
		pattern: joinPaths(r.basePath, path),
	})
	return fixed
}

func (r *Router) Handle(method, path string, h httpx.Handler) {
	r.group.Add(strings.ToUpper(method), r.normalizeWildcardPath(path), r.toEchoHandler(h))
}

// HandleStd mounts a plain net/http handler, implementing httpx.StdHandlerMounter.
// It goes through Handle so interceptors registered on this scope also wrap it.
func (r *Router) HandleStd(method, path string, h http.Handler) {
	r.Handle(method, path, stdLeaf(h))
}

func (r *Router) Any(path string, h httpx.Handler) {
	r.group.Any(r.normalizeWildcardPath(path), r.toEchoHandler(h))
}

func (r *Router) Static(prefix, root string) {
	r.StaticFS(prefix, os.DirFS(root))
}

// StaticFS serves filesystem through httpx.StaticFileHandler and registers it
// as an ordinary route, so interceptors on this scope wrap static requests
// too. The named-wildcard pattern (rather than echo's native prefix+"*")
// keeps adjacent URLs like /assetshello.txt from matching and keeps HEAD from
// returning 405.
func (r *Router) StaticFS(prefix string, filesystem fs.FS) {
	pattern := httpx.StaticRoutePattern(prefix)
	leaf := stdLeaf(httpx.StaticFileHandler(joinPaths(r.basePath, prefix), filesystem))
	r.Handle(http.MethodGet, pattern, leaf)
	r.Handle(http.MethodHead, pattern, leaf)
}

// stdLeaf serves a plain net/http handler through echo's response and request,
// so a std handler can sit at the end of a composed interceptor chain.
func stdLeaf(h http.Handler) httpx.Handler {
	return func(ctx httpx.Context) error {
		ec, ok := httpx.AsNativeContext[echo.Context](ctx)
		if !ok {
			return errors.New("echox: echo context type error")
		}
		h.ServeHTTP(ec.Response(), ec.Request())
		return nil
	}
}

// GET registers a new GET route for a path with matching handler.
func (r *Router) GET(path string, h httpx.Handler) {
	r.Handle(http.MethodGet, path, h)
}

// POST registers a new POST route for a path with matching handler.
func (r *Router) POST(path string, h httpx.Handler) {
	r.Handle(http.MethodPost, path, h)
}

// PUT registers a new PUT route for a path with matching handler.
func (r *Router) PUT(path string, h httpx.Handler) {
	r.Handle(http.MethodPut, path, h)
}

// DELETE registers a new DELETE route for a path with matching handler.
func (r *Router) DELETE(path string, h httpx.Handler) {
	r.Handle(http.MethodDelete, path, h)
}

// PATCH registers a new PATCH route for a path with matching handler.
func (r *Router) PATCH(path string, h httpx.Handler) {
	r.Handle(http.MethodPatch, path, h)
}

// HEAD registers a new HEAD route for a path with matching handler.
func (r *Router) HEAD(path string, h httpx.Handler) {
	r.Handle(http.MethodHead, path, h)
}

// OPTIONS registers a new OPTIONS route for a path with matching handler.
func (r *Router) OPTIONS(path string, h httpx.Handler) {
	r.Handle(http.MethodOptions, path, h)
}

func (r *Router) toEchoHandler(h httpx.Handler) echo.HandlerFunc {
	// Composed once per route, never per request.
	h = httpx.ComposeInterceptors(h, r.interceptors)
	return func(ec echo.Context) error {
		ctx := newEchoContext(ec, r.wildcards)
		if err := h(ctx); err != nil {
			// Without a framework-neutral handler the error goes to echo's own
			// path. So does an error after a committed response: nothing may
			// write over it, but logging middleware must still see it.
			if r.errHandler == nil || ec.Response().Committed {
				return err
			}
			r.errHandler(ctx, err)
			commitErrorStatus(ec.Response(), err)
			return nil
		}
		// A handler that only set the status still owes a response; echo itself
		// would let it fall out as 200.
		if resp := ec.Response(); !resp.Committed {
			resp.WriteHeader(resp.Status)
		}
		return nil
	}
}

// commitErrorStatus commits the response for an error the configured
// httpx.ErrorHandler rendered nothing for.
//
// Rendering nothing is a legitimate shape — a handler that only logs, and
// leaves the body to a layer above — but nothing runs below this point, and
// echo's Response.Status is still its initial 200 for a request that reached no
// handler. Committing that answered an unmatched path with 200, which caches
// and monitoring believe. The error's own status is the floor; a status the
// error handler set for itself still wins, and a response it committed is never
// touched.
func commitErrorStatus(resp *echo.Response, err error) {
	if resp.Committed {
		return
	}
	if resp.Status == http.StatusOK {
		// Classify the normalized error so echo's own 404/405 keeps its status
		// instead of being reported as an unclassified 500.
		_, status, _ := httpx.ClassifyError(normalizeEchoError(err))
		resp.Status = int(status)
	}
	resp.WriteHeader(resp.Status)
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
