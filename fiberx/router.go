package fiberx

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
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/adaptor"
)

var (
	_ httpx.Router           = (*Router)(nil)
	_ httpx.InterceptorScope = (*Router)(nil)
)

// wildcardNames maps a registered route pattern (with the anonymous "*"
// wildcard) to the original named wildcard parameter, so Param(name) keeps
// working after FixWildcardPathIfNeed rewrote the path.
var wildcardNames sync.Map // route pattern -> original param name

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
// anonymous form (/*) and records the original name so Param("filepath")
// still resolves. Unsupported wildcard shapes panic at registration,
// matching gin/hertz native behavior (fiber would otherwise silently
// register a semantically different route).
func (r *Router) normalizeWildcardPath(path string) string {
	if err := httpx.ValidateWildcardPath(path); err != nil {
		panic(err)
	}
	orig := httpx.WildcardParamName(path)
	if orig == "" || orig == "*" {
		return path
	}
	fixed, _ := httpx.FixWildcardPathIfNeed(r, path)
	wildcardNames.Store(joinPaths(r.basePath, fixed), orig)
	return fixed
}

func (r *Router) Handle(method, path string, h httpx.Handler) {
	methods := []string{strings.ToUpper(method)}
	handler, handlers := splitHandlers(r.adaptHandler(h))
	r.group.Add(methods, r.normalizeWildcardPath(path), handler, handlers...)
}

// HandleStd mounts a plain net/http handler, implementing httpx.StdHandlerMounter.
// It goes through Handle so interceptors registered on this scope also wrap it.
func (r *Router) HandleStd(method, path string, h http.Handler) {
	r.Handle(method, path, stdLeaf(h))
}

func (r *Router) Any(path string, h httpx.Handler) {
	handler, handlers := splitHandlers(r.adaptHandler(h))
	r.group.All(r.normalizeWildcardPath(path), handler, handlers...)
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

func (r *Router) combineHandlers(h fiber.Handler) []any {
	mid := make([]any, 0, len(r.middlewares)+1)
	for _, m := range r.middlewares {
		mid = append(mid, adaptMiddleware(m, r.errHandler))
	}
	mid = append(mid, h)
	return mid
}

func (r *Router) adaptHandler(h httpx.Handler) []any {
	// Composed once per route, never per request.
	h = httpx.ComposeInterceptors(h, r.interceptors)
	return r.combineHandlers(func(ctx fiber.Ctx) error {
		fc := newFiberContext(ctx)
		return handleFiberError(ctx, fc, h(fc), r.errHandler)
	})
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
		errHandler(fc, err)
		// Rendered, but still recorded: gin and hertz keep a handled error on
		// the native error list, so an outer layer's Next sees it. Without
		// this, rendering at the failing layer would hide the failure from
		// logging middleware registered above it.
		native.Locals(handledErrorKey{}, err)
		return nil
	}
	return err
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
