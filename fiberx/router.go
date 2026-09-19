package fiberx

import (
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path"
	"slices"
	"strings"

	"github.com/go-sphere/httpx"
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/adaptor"
)

var (
	_ httpx.Router           = (*Router)(nil)
	_ httpx.InterceptorScope = (*Router)(nil)
)

// wildcardRoute remembers what a route looked like before
// FixWildcardPathIfNeed rewrote its named wildcard to the anonymous form fiber
// supports: the original parameter name, so Param(name) keeps resolving, and
// the pattern as the caller registered it, so FullPath reports that rather than
// this adapter's normalization.
type wildcardRoute struct {
	param   string
	pattern string
}

// wildcardRouteKey names the Locals slot holding the matched route's
// named-wildcard mapping. An unexported struct type cannot collide with an
// application's own Locals keys.
type wildcardRouteKey struct{}

// markWildcardRoute records a route's wildcard mapping on the request before
// next runs, so every reading of the parameter set resolves the name the route
// was registered with.
//
// The mapping belongs to one route and is known at registration, but a fiber
// context cannot carry it: fiberContext holds a single pointer so it fits in an
// interface without a heap wrapper (see its doc), and a second field would cost
// an allocation on every request. Fiber's own request-local storage is the next
// cheapest place, and only a route that *has* a named wildcard is wrapped, so
// an ordinary route pays nothing at all.
//
// This replaces a process-wide "normalized pattern -> name" map. Normalization
// is lossy — /files/*filename and /files/*filepath both become /files/* — so two
// engines in one process overwrote each other's entry and the last registration
// answered for both, silently reporting another engine's pattern from FullPath.
// Attaching the mapping to the route makes it per engine by construction.
func markWildcardRoute(next fiber.Handler, route *wildcardRoute) fiber.Handler {
	return func(ctx fiber.Ctx) error {
		ctx.Locals(wildcardRouteKey{}, route)
		return next(ctx)
	}
}

// wildcardRouteOf returns the matched route's wildcard mapping, or nil when the
// route has none — including on a context built by FromFiber for a request this
// adapter did not route.
func wildcardRouteOf(native fiber.Ctx) *wildcardRoute {
	route, _ := native.Locals(wildcardRouteKey{}).(*wildcardRoute)
	return route
}

type Router struct {
	basePath     string
	group        fiber.Router
	middlewares  []httpx.Middleware
	errHandler   httpx.ErrorHandler
	interceptors []httpx.Interceptor
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
	r.middlewares = append(r.middlewares, m...)
}

// UseNative registers native fiber middleware on this group. Prefer it over
// wrapping a fiber.Handler with AdaptFiberMiddleware: the handler runs with no
// adapter in between.
//
// Two fiber-specific rules apply here that do not on gin, echo or hertz.
// Register it before the routes it should wrap: fiber matches its route stack
// in registration order, so a native middleware added after a route is reached
// only if that route's handler calls Next. And it always runs outside the
// middleware registered with Use on this scope, whatever the call order —
// unlike gin, where the two interleave — because Use composes into each route
// at registration while this occupies a real fiber stack entry ahead of it.
func (r *Router) UseNative(handlers ...fiber.Handler) {
	for _, h := range handlers {
		r.group.Use(h)
	}
}

func (r *Router) BasePath() string {
	if r.basePath == "" {
		return "/"
	}
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
		basePath:     joinPaths(r.basePath, prefix),
		group:        r.group.Group(prefix),
		middlewares:  cloneMiddlewares(r.middlewares, m...),
		errHandler:   r.errHandler,
		interceptors: r.interceptors,
	}
}

// normalizeWildcardPath rewrites named wildcards (/*filepath) to fiber's
// anonymous form (/*) and returns what the route was written as, so
// Param("filepath") still resolves; the second result is nil for a route with
// no named wildcard. Unsupported wildcard shapes panic at registration,
// matching gin/hertz native behavior (fiber would otherwise silently
// register a semantically different route).
func (r *Router) normalizeWildcardPath(path string) (string, *wildcardRoute) {
	if err := httpx.ValidateWildcardPath(path); err != nil {
		panic(err)
	}
	orig := httpx.WildcardParamName(path)
	if orig == "" || orig == "*" {
		return path, nil
	}
	fixed, _ := httpx.FixWildcardPathIfNeed(r, path)
	return fixed, &wildcardRoute{
		param:   orig,
		pattern: joinPaths(r.basePath, path),
	}
}

func (r *Router) Handle(method, path string, h httpx.Handler) {
	methods := []string{strings.ToUpper(method)}
	fixed, wildcard := r.normalizeWildcardPath(path)
	handler, handlers := splitHandlers(r.adaptHandler(h, wildcard))
	r.group.Add(methods, fixed, handler, handlers...)
}

// HandleStd mounts a plain net/http handler, implementing httpx.StdHandlerMounter.
// It goes through Handle so interceptors registered on this scope also wrap it.
func (r *Router) HandleStd(method, path string, h http.Handler) {
	r.Handle(method, path, stdLeaf(h))
}

func (r *Router) Any(path string, h httpx.Handler) {
	fixed, wildcard := r.normalizeWildcardPath(path)
	handler, handlers := splitHandlers(r.adaptHandler(h, wildcard))
	r.group.All(fixed, handler, handlers...)
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

// stdLeaf serves a plain net/http handler through fiber's context, so a std
// handler can sit at the end of a composed interceptor chain.
func stdLeaf(h http.Handler) httpx.Handler {
	native := adaptor.HTTPHandler(h)
	return func(ctx httpx.Context) error {
		fc, ok := httpx.AsNativeContext[fiber.Ctx](ctx)
		if !ok {
			return errors.New("fiberx: fiber context type error")
		}
		err := native(fc)
		if err == nil {
			markResponseCommitted(fc)
		}
		return err
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

func (r *Router) combineHandlers(h fiber.Handler, wildcard *wildcardRoute) []any {
	chain := make([]fiber.Handler, 0, len(r.middlewares)+1)
	for _, m := range r.middlewares {
		chain = append(chain, adaptMiddleware(m, r.errHandler))
	}
	chain = append(chain, h)
	if wildcard != nil {
		// Marked on the *first* layer of the route, not on the handler: Use
		// composes into the route here rather than taking a native slot, so a
		// middleware registered with it runs inside this chain and must read
		// the same FullPath and Param as the handler below it.
		chain[0] = markWildcardRoute(chain[0], wildcard)
	}
	mid := make([]any, len(chain))
	for i, handler := range chain {
		mid[i] = handler
	}
	return mid
}

func (r *Router) adaptHandler(h httpx.Handler, wildcard *wildcardRoute) []any {
	// Composed once per route, never per request.
	h = httpx.ComposeInterceptors(h, r.interceptors)
	return r.combineHandlers(func(ctx fiber.Ctx) error {
		fc := newFiberContext(ctx)
		return handleFiberError(ctx, fc, h(fc), r.errHandler)
	}, wildcard)
}

// handleFiberError routes a handler/middleware error either through the
// configured httpx.ErrorHandler (with a real httpx.Context) or back to
// fiber's error handling. A committed response is never overwritten.
func handleFiberError(native fiber.Ctx, fc httpx.Context, err error, errHandler httpx.ErrorHandler) error {
	if err == nil {
		return nil
	}
	if responseDecided(native) {
		// The handler already decided the response; replacing it with an
		// error body would corrupt it. Match the committed-response behavior
		// of the other adapters and leave it untouched — but keep the error
		// reachable instead of dropping it, which is what the other three do
		// (gin/hertz put it on the native error list, echo returns it to its
		// own error path). Returning it to fiber is not an option here: fiber's
		// ErrorHandler would render it over the committed body.
		native.Locals(handledErrorKey{}, err)
		return nil
	}
	if errHandler != nil {
		// The error may be fiber's own (a middleware whose Next fell through to
		// an unmatched path): normalize it so the configured handler sees
		// 404/405 rather than an unclassified 500.
		errHandler(fc, normalizeFiberError(err))
		commitErrorStatus(native, err)
		// Rendered, but still recorded: gin and hertz keep a handled error on
		// the native error list, so an outer layer's Next sees it. Without
		// this, rendering at the failing layer would hide the failure from
		// logging middleware registered above it.
		native.Locals(handledErrorKey{}, err)
		return nil
	}
	return err
}

// commitErrorStatus gives the response the error's own status when the
// configured httpx.ErrorHandler rendered nothing for it.
//
// Rendering nothing is a legitimate shape — a handler that only logs, and
// leaves the body to a layer above — but the error is swallowed here (fiber
// must not be allowed to render it a second time), so the status fiber would
// send is its initial 200. On an unmatched path that answered a request no
// route handled with 200, which caches and monitoring believe. The error's own
// status is the floor; a status the error handler set for itself still wins,
// and a response it decided is never touched.
func commitErrorStatus(native fiber.Ctx, err error) {
	if responseDecided(native) || native.Response().StatusCode() != fiber.StatusOK {
		return
	}
	// Classify the normalized error so fiber's own 404/405 keeps its status
	// instead of being reported as an unclassified 500.
	_, status, _ := httpx.ClassifyError(normalizeFiberError(err))
	native.Status(int(status))
}

// handledErrorKey names the Locals slot holding an error that could not be
// rendered because the response was already decided. An unexported struct type
// cannot collide with an application's own Locals keys.
type handledErrorKey struct{}

// committedError returns the error a decided response prevented from being
// rendered, if any. Context.Next surfaces it so an outer layer still sees the
// failure it would have seen on the other three adapters.
func handledError(native fiber.Ctx) error {
	err, _ := native.Locals(handledErrorKey{}).(error)
	return err
}

// responseDecided reports whether the handler already produced a response.
//
// A bodyless response is decided without writing any bytes, so "has a body"
// cannot detect it: 204/304/1xx carry no body by definition, and a redirect
// carries only a Location. A bare Status(code) is deliberately *not* counted —
// it records a code without producing a response, and swallowing an error
// behind it would turn a failure into a silent 2xx.
func responseDecided(native fiber.Ctx) bool {
	if native.Response().IsBodyStream() || len(native.Response().Body()) > 0 {
		return true
	}
	committed, _ := native.Locals(responseCommittedKey{}).(bool)
	return committed
}

type responseCommittedKey struct{}

func markResponseCommitted(native fiber.Ctx) { native.Locals(responseCommittedKey{}, true) }

func splitHandlers(handlers []any) (any, []any) {
	if len(handlers) == 0 {
		return nil, nil
	}
	if len(handlers) == 1 {
		return handlers[0], nil
	}
	return handlers[0], handlers[1:]
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
