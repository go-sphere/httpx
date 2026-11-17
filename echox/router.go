package echox

import (
	"errors"
	"io/fs"
	"maps"
	"net/http"
	"os"
	"path"
	"strings"
	"sync"
	"sync/atomic"

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

// wildcardTable maps a method and a normalized route pattern (with the
// anonymous "*" wildcard) to what the route was written as.
//
// The method is part of the key because echo stores one anonymous wildcard
// node per pattern and every method's handler under it: GET /files/*a and
// POST /files/*b both match as "/files/*", and each must keep its own name, as
// it does on the adapters with native named wildcards.
//
// One table per engine, never one per process. Normalization is lossy — both
// /files/*filepath and /files/*path become /files/* — so a process-wide table
// would let the engine that registered last own the entry for every engine
// sharing the normalized form, and FullPath would silently report another
// engine's pattern, which is what downstream auth and rate limiting match on.
// New creates the table and hands it to every Router made from it, so a Group
// shares its engine's table and two engines share nothing.
//
// Registration copies the map and publishes the copy, so a lookup on the
// request path takes no lock and allocates nothing.
type wildcardTable struct {
	mu     sync.Mutex
	routes atomic.Pointer[map[wildcardKey]wildcardRoute]
}

type wildcardKey struct {
	method  string
	pattern string
}

// set records route for method and the normalized pattern, or removes the
// entry when route has no name: re-registering the pattern with an anonymous
// wildcard replaces the named route in echo too.
func (t *wildcardTable) set(method, pattern string, route wildcardRoute) {
	t.mu.Lock()
	defer t.mu.Unlock()
	key := wildcardKey{method: method, pattern: pattern}
	var cur map[wildcardKey]wildcardRoute
	if p := t.routes.Load(); p != nil {
		cur = *p
	}
	if _, ok := cur[key]; !ok && route.param == "" {
		return
	}
	next := make(map[wildcardKey]wildcardRoute, len(cur)+1)
	maps.Copy(next, cur)
	if route.param == "" {
		delete(next, key)
	} else {
		next[key] = route
	}
	t.routes.Store(&next)
}

// lookup resolves a matched route pattern back to its registered form for the
// request's method. The caller checks the pattern ends in "*" first, so a
// route without a wildcard never pays the map lookup. A nil table (a context
// built by FromEcho, which has no engine behind it) resolves nothing.
func (t *wildcardTable) lookup(method, pattern string) (wildcardRoute, bool) {
	if t == nil {
		return wildcardRoute{}, false
	}
	routes := t.routes.Load()
	if routes == nil {
		return wildcardRoute{}, false
	}
	route, ok := (*routes)[wildcardKey{method: method, pattern: pattern}]
	return route, ok
}

// Router is the echo route scope returned by [Engine.Group] and
// [Router.Group], wrapping an *echo.Group. Its zero value is not usable.
// Register routes before the engine starts serving.
type Router struct {
	group    *echo.Group
	basePath string
	// errHandler is the engine's routeErrHandler: never nil, and it takes the
	// error as the route returned it.
	errHandler httpx.ErrorHandler
	// chain references the engine's chain rather than copying it; see
	// httpx.MiddlewareChain.
	chain *httpx.MiddlewareChain
	// wildcards is the engine's table, shared with every Group made from it.
	wildcards   *wildcardTable
	maxBodySize int64
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

// BasePath returns this scope's absolute prefix, the group prefixes joined
// with a trailing slash the caller wrote kept.
func (r *Router) BasePath() string {
	return r.basePath
}

// SupportsRouterFeature reports every feature as unsupported natively. Named
// wildcards still work: Handle rewrites them to echo's anonymous form and
// keeps the name for Param, BindURI and FullPath.
func (r *Router) SupportsRouterFeature(httpx.RouterFeature) bool {
	return false
}

// Group returns a nested scope under prefix with m registered on it as if by
// Use.
func (r *Router) Group(prefix string, m ...httpx.Middleware) httpx.Router {
	base := joinPaths(r.basePath, prefix)
	sub := r.chain.Sub()
	sub.Use(m...)
	return &Router{
		group:       r.group.Group(echoGroupPrefix(r.basePath, base)),
		basePath:    base,
		errHandler:  r.errHandler,
		chain:       sub,
		wildcards:   r.wildcards,
		maxBodySize: r.maxBodySize,
	}
}

// normalizeWildcardPath rewrites a named wildcard (/*filepath) to echo's
// anonymous form (/*) and returns the rewritten path with what the route was
// written as; route.param is empty when the path has no named wildcard.
// Unsupported wildcard shapes panic at registration, matching gin/hertz native
// behavior.
func (r *Router) normalizeWildcardPath(path string) (string, wildcardRoute) {
	if err := httpx.ValidateWildcardPath(path); err != nil {
		panic(err)
	}
	joined := joinPaths(r.basePath, path)
	if err := httpx.ValidateWildcardPath(joined); err != nil {
		panic(err)
	}
	orig := httpx.WildcardParamName(path)
	if orig == "" || orig == "*" {
		return path, wildcardRoute{}
	}
	fixed, _ := httpx.FixWildcardPathIfNeed(r, path)
	return fixed, wildcardRoute{
		param:   orig,
		pattern: joined,
	}
}

// Handle registers h for method (upper-cased) and path joined onto BasePath,
// wrapped in the httpx middleware registered so far on this scope and its
// parents. A named wildcard is rewritten for echo and its name kept for this
// method. It panics on an invalid wildcard (httpx.ValidateWildcardPath).
func (r *Router) Handle(method, path string, h httpx.Handler) {
	fixed, wildcard := r.normalizeWildcardPath(path)
	route := r.group.Add(strings.ToUpper(method), echoRoutePath(r.basePath, fixed), r.toEchoHandler(h))
	r.recordWildcard(route, wildcard)
}

// recordWildcard keeps the name a route's wildcard was registered with, under
// the method and pattern echo matches it by. A pattern without a trailing
// wildcard is never looked up, so it is not recorded either.
func (r *Router) recordWildcard(route *echo.Route, wildcard wildcardRoute) {
	if route == nil || !lastCharIs('*', route.Path) {
		return
	}
	r.wildcards.set(route.Method, route.Path, wildcard)
}

// HandleStd mounts a plain net/http handler, implementing httpx.StdHandlerMounter.
// It goes through Handle so middleware registered on this scope also wraps it.
func (r *Router) HandleStd(method, path string, h http.Handler) {
	r.Handle(method, path, stdLeaf(h))
}

// Any registers h for path with echo's Any, so the method set beyond the
// standard ones is echo's.
func (r *Router) Any(path string, h httpx.Handler) {
	fixed, wildcard := r.normalizeWildcardPath(path)
	for _, route := range r.group.Any(echoRoutePath(r.basePath, fixed), r.toEchoHandler(h)) {
		r.recordWildcard(route, wildcard)
	}
}

// Static serves the directory root at prefix; it is StaticFS with
// os.DirFS(root).
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
		orig := ec.Request()
		defer func() { removeReplacedMultipartForm(orig, ec.Request()) }()
		err := httpx.LimitRequestBody(ec.Response(), ec.Request(), r.maxBodySize)
		if err == nil {
			err = h(ctx)
		}
		if err != nil {
			// An error after a committed response goes to echo's own path:
			// nothing may write over it, but logging middleware must still see
			// it.
			if ec.Response().Committed {
				// Marked so the engine's error handler does not mistake an
				// echo.ErrNotFound a route returned for an unmatched path.
				ec.Set(routeErrorKey, true)
				return err
			}
			// Rendered here rather than returned, so a UseNative layer sees the
			// response the error produced, as it does on ginx.
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

// removeReplacedMultipartForm deletes the temporary files of a multipart form
// parsed on cur, a request that replaced orig during the chain (SetContext, a
// net/http middleware). net/http removes them only for the request it served,
// so without this an upload spilled to disk would stay there.
func removeReplacedMultipartForm(orig, cur *http.Request) {
	if cur != orig && cur.MultipartForm != nil && cur.MultipartForm != orig.MultipartForm {
		_ = cur.MultipartForm.RemoveAll()
	}
}
