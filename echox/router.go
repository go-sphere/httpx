package echox

import (
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path"
	"strings"
	"sync"

	"github.com/go-sphere/httpx"
	"github.com/labstack/echo/v4"
)

var _ httpx.Router = (*Router)(nil)

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
// One table per engine, never one per process. Normalization is lossy — both
// /files/*filepath and /files/*path become /files/* — so a process-wide table
// would let the engine that registered last own the entry for every engine
// sharing the normalized form, and FullPath would silently report another
// engine's pattern, which is what downstream auth and rate limiting match on.
// New creates the table and hands it to every Router made from it, so a Group
// shares its engine's table and two engines share nothing.
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
	group      *echo.Group
	basePath   string
	errHandler httpx.ErrorHandler
	// chain references the engine's chain rather than copying it; see
	// httpx.MiddlewareChain.
	chain *httpx.MiddlewareChain
	// wildcards is the engine's table, shared with every Group made from it.
	wildcards *wildcardTable
}

// Use registers httpx middleware on this scope, implementing
// httpx.MiddlewareScope. The chain is composed into every route registered
// afterwards, so it needs no native handler slot and no per-request object; see
// httpx.Middleware and httpx.MiddlewareChain for the ordering rules.
func (r *Router) Use(m ...httpx.Middleware) {
	r.chain.Use(m...)
}

// UseNative registers native echo middleware on this group, the only way to
// mount an echo.MiddlewareFunc: it gets its own echo middleware slot, which an
// httpx.Middleware wrapper could not reproduce — all httpx layers share the
// route handler's single slot, so the next echo.HandlerFunc a wrapped
// middleware calls is the whole composed chain rather than the layer below it.
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
	base := joinPaths(r.basePath, prefix)
	sub := r.chain.Sub()
	sub.Use(m...)
	return &Router{
		group:      r.group.Group(echoGroupPrefix(r.basePath, base)),
		basePath:   base,
		errHandler: r.errHandler,
		chain:      sub,
		wildcards:  r.wildcards,
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
	r.group.Add(strings.ToUpper(method), r.echoPath(path), r.toEchoHandler(h))
}

// echoPath is the registration path for this scope: the caller's path with any
// named wildcard normalized to echo's anonymous form, then rebased so
// concatenation onto the group prefix reproduces joinPaths(basePath, path).
func (r *Router) echoPath(path string) string {
	return echoRoutePath(r.basePath, r.normalizeWildcardPath(path))
}

// HandleStd mounts a plain net/http handler, implementing httpx.StdHandlerMounter.
// It goes through Handle so middleware registered on this scope also wraps it.
func (r *Router) HandleStd(method, path string, h http.Handler) {
	r.Handle(method, path, stdLeaf(h))
}

func (r *Router) Any(path string, h httpx.Handler) {
	r.group.Any(r.echoPath(path), r.toEchoHandler(h))
}

func (r *Router) Static(prefix, root string) {
	r.StaticFS(prefix, os.DirFS(root))
}

// StaticFS serves filesystem through httpx.StaticFileHandler and registers it
// as an ordinary route, so middleware on this scope wraps static requests
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
// so a std handler can sit at the end of a composed middleware chain.
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
	h = r.chain.Compose(h)
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

// echo joins a group prefix and a route path by plain concatenation —
// Group.Add registers g.prefix+path, Group.Group registers g.prefix+prefix — so
// a prefix the caller wrote with a trailing slash puts a second slash into every
// route under it, and Group("/") plus GET("/x") registers "//x". The other four
// adapters join through path.Join and never see it, and BasePath() reports the
// right thing either way, so the symptom is a silent 404.
//
// The three functions below are why that cannot happen here: basePath stays the
// caller's string cleaned by joinPaths, exactly as on the other adapters, and
// what echo is handed is derived from it — never the caller's prefix directly.
//
// The invariant: an echo group's prefix is always echoScopePrefix(basePath), so
// the parent's is a prefix of the child's and a route's registered pattern is
// always joinPaths(basePath, routePath).

// echoScopePrefix is the prefix an echo group carries for a scope whose base
// path is base. joinPaths has already made base absolute and free of empty or
// dotted segments, so the trailing slash is the only thing concatenation cannot
// survive.
func echoScopePrefix(base string) string {
	return strings.TrimSuffix(base, "/")
}

// echoGroupPrefix is what echo's Group needs for a child scope: the part this
// scope's prefix adds to its parent's.
func echoGroupPrefix(parentBase, base string) string {
	return strings.TrimPrefix(echoScopePrefix(base), echoScopePrefix(parentBase))
}

// echoRoutePath is what echo's Add needs for a route registered as routePath on
// a scope whose base path is base: the part the route adds to the group prefix.
// Deriving it from joinPaths rather than passing routePath through is what keeps
// the empty route path right — Group("/api/").GET("") answers /api/ on the other
// four, while concatenating "" onto the trimmed prefix would register /api.
func echoRoutePath(base, routePath string) string {
	return strings.TrimPrefix(joinPaths(base, routePath), echoScopePrefix(base))
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
