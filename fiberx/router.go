package fiberx

import (
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path"
	"strings"

	"github.com/go-sphere/httpx"
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/adaptor"
)

var _ httpx.Router = (*Router)(nil)

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
// next runs, so every reading of the parameter set — including from a layer of
// the route's own composed chain — resolves the name the route was registered
// with.
//
// The mapping belongs to one route and is known at registration, but a fiber
// context cannot carry it: fiberContext holds a single pointer so it fits in an
// interface without a heap wrapper (see its doc), and a second field would cost
// an allocation on every request. Fiber's own request-local storage is the next
// cheapest place, and only a route that has a named wildcard is wrapped, so an
// ordinary route pays nothing at all.
//
// Attaching the mapping to the route is also what makes it per engine by
// construction. A process-wide "normalized pattern -> name" map cannot be:
// normalization is lossy — /files/*filename and /files/*filepath both become
// /files/* — so two engines in one process would overwrite each other's entry
// and FullPath would silently report another engine's pattern.
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
	basePath   string
	group      fiber.Router
	errHandler httpx.ErrorHandler
	// chain references the engine's chain rather than copying it; see
	// httpx.MiddlewareChain.
	chain *httpx.MiddlewareChain
}

// Use registers httpx middleware on this scope, implementing
// httpx.MiddlewareScope. The chain is composed into every route registered
// afterwards, so it needs no native handler slot and no per-request object; see
// httpx.Middleware and httpx.MiddlewareChain for the ordering rules.
func (r *Router) Use(m ...httpx.Middleware) {
	r.chain.Use(m...)
}

// UseNative registers native fiber middleware on this group, the only way to
// mount a fiber.Handler: it gets its own fiber stack entry, which an
// httpx.Middleware wrapper could not reproduce — all httpx layers share the
// route's single entry, so the wrapped handler's c.Next() would advance fiber
// past that entry rather than into the httpx chain.
//
// One fiber-specific rule applies here that does not on gin, echo or hertz:
// register it before the routes it should wrap, because fiber matches its route
// stack in registration order, so a native middleware added after a route is
// reached only if that route's handler calls Next.
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
	sub := r.chain.Sub()
	sub.Use(m...)
	return &Router{
		basePath:   joinPaths(r.basePath, prefix),
		group:      r.group.Group(prefix),
		errHandler: r.errHandler,
		chain:      sub,
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
	r.group.Add(methods, fixed, r.adaptHandler(h, wildcard))
}

// HandleStd mounts a plain net/http handler, implementing httpx.StdHandlerMounter.
// It goes through Handle so middleware registered on this scope also wraps it.
func (r *Router) HandleStd(method, path string, h http.Handler) {
	r.Handle(method, path, stdLeaf(h))
}

func (r *Router) Any(path string, h httpx.Handler) {
	fixed, wildcard := r.normalizeWildcardPath(path)
	r.group.All(fixed, r.adaptHandler(h, wildcard))
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

// stdLeaf serves a plain net/http handler through fiber's context, so a std
// handler can sit at the end of a composed middleware chain.
func stdLeaf(h http.Handler) httpx.Handler {
	return func(ctx httpx.Context) error {
		fc, ok := httpx.AsNativeContext[fiber.Ctx](ctx)
		if !ok {
			return errors.New("fiberx: fiber context type error")
		}
		// fasthttpadaptor hands the handler fasthttp's RequestCtx as
		// r.Context(), which knows nothing about the context httpx layers set;
		// fiber's context is that chain's continuation, so it is swapped in
		// before the handler runs.
		err := adaptor.HTTPHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h.ServeHTTP(w, r.WithContext(fc.Context()))
		}))(fc)
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

// adaptHandler builds the fiber handler for one route: the composed httpx chain
// with the error plumbing around it, plus the named-wildcard mark when the route
// has one.
func (r *Router) adaptHandler(h httpx.Handler, wildcard *wildcardRoute) fiber.Handler {
	// Composed once per route, never per request.
	h = r.chain.Compose(h)
	handler := fiber.Handler(func(ctx fiber.Ctx) error {
		fc := newFiberContext(ctx)
		return handleFiberError(ctx, fc, h(fc), r.errHandler)
	})
	if wildcard != nil {
		handler = markWildcardRoute(handler, wildcard)
	}
	return handler
}

// handleFiberError routes a handler or middleware error either through the
// configured httpx.ErrorHandler (with a real httpx.Context) or back to fiber's
// error handling. A committed response is never overwritten.
func handleFiberError(native fiber.Ctx, fc httpx.Context, err error, errHandler httpx.ErrorHandler) error {
	if err == nil {
		return nil
	}
	if responseDecided(native) {
		// The handler already decided the response; replacing it with an
		// error body would corrupt it. Match the committed-response behavior
		// of the other adapters and leave it untouched. Returning it to fiber
		// is not an option here: fiber's ErrorHandler would render it over the
		// committed body. Nothing is lost by dropping it — every layer that
		// could care already saw it on its way out of the composed chain.
		return nil
	}
	if errHandler != nil {
		// The error may be fiber's own (a route handler that returned one):
		// normalize it so the configured handler sees 404/405 rather than an
		// unclassified 500.
		errHandler(fc, normalizeFiberError(err))
		commitErrorStatus(native, err)
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
// send is its initial 200, and an unmatched path would answer 200. The error's
// own status is the floor; a status the error handler set for itself still wins,
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
