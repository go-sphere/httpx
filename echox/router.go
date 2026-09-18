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

// wildcardNames maps a registered route pattern (with the anonymous "*"
// wildcard) to the original named wildcard parameter, so Param(name) keeps
// working after FixWildcardPathIfNeed rewrote the path.
var wildcardNames sync.Map // route pattern -> original param name

type Router struct {
	group        *echo.Group
	basePath     string
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
	r.group.Use(adaptMiddlewares(m, r.errHandler)...)
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
		group:        r.group.Group(prefix, adaptMiddlewares(m, r.errHandler)...),
		basePath:     joinPaths(r.basePath, prefix),
		errHandler:   r.errHandler,
		interceptors: r.interceptors,
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
	wildcardNames.Store(joinPaths(r.basePath, fixed), orig)
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
		ctx := newEchoContext(ec)
		if err := h(ctx); err != nil {
			// Without a framework-neutral handler the error goes to echo's own
			// path. So does an error after a committed response: nothing may
			// write over it, but logging middleware must still see it.
			if r.errHandler == nil || ec.Response().Committed {
				return err
			}
			r.errHandler(ctx, err)
		}
		// A handler — or an error handler — that only set the status still
		// owes a response; echo itself would let it fall out as 200.
		if resp := ec.Response(); !resp.Committed {
			resp.WriteHeader(resp.Status)
		}
		return nil
	}
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
