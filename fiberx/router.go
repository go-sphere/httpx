package fiberx

import (
	"io/fs"
	"net/http"
	"path"
	"strings"
	"sync"

	"github.com/go-sphere/httpx"
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/adaptor"
	"github.com/gofiber/fiber/v3/middleware/static"
)

var _ httpx.Router = (*Router)(nil)

// wildcardNames maps a registered route pattern (with the anonymous "*"
// wildcard) to the original named wildcard parameter, so Param(name) keeps
// working after FixWildcardPathIfNeed rewrote the path.
var wildcardNames sync.Map // route pattern -> original param name

type Router struct {
	basePath    string
	group       fiber.Router
	middlewares []httpx.Middleware
	errHandler  httpx.ErrorHandler
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
		basePath:    joinPaths(r.basePath, prefix),
		group:       r.group.Group(prefix),
		middlewares: cloneMiddlewares(r.middlewares, m...),
		errHandler:  r.errHandler,
	}
}

// normalizeWildcardPath rewrites named wildcards (/*filepath) to fiber's
// anonymous form (/*) and records the original name so Param("filepath")
// still resolves.
func (r *Router) normalizeWildcardPath(path string) string {
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
func (r *Router) HandleStd(method, path string, h http.Handler) {
	methods := []string{strings.ToUpper(method)}
	handler, handlers := splitHandlers(r.combineHandlers(adaptor.HTTPHandler(h)))
	r.group.Add(methods, r.normalizeWildcardPath(path), handler, handlers...)
}

func (r *Router) Any(path string, h httpx.Handler) {
	handler, handlers := splitHandlers(r.adaptHandler(h))
	r.group.All(r.normalizeWildcardPath(path), handler, handlers...)
}

func (r *Router) Static(prefix, root string) {
	r.group.Use(append([]any{prefix}, r.combineHandlers(static.New(root))...)...)
}

func (r *Router) StaticFS(prefix string, fs fs.FS) {
	r.group.Use(append([]any{prefix}, r.combineHandlers(static.New("", static.Config{FS: fs}))...)...)
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
	return r.combineHandlers(func(ctx fiber.Ctx) error {
		fc := newFiberContext(ctx)
		return handleFiberError(fc, h(fc), r.errHandler)
	})
}

// handleFiberError routes a handler/middleware error either through the
// configured httpx.ErrorHandler (with a real httpx.Context) or back to
// fiber's error handling. A committed response is never overwritten.
func handleFiberError(fc *fiberContext, err error, errHandler httpx.ErrorHandler) error {
	if err == nil {
		return nil
	}
	if len(fc.ctx.Response().Body()) > 0 {
		// The handler already wrote a body; replacing it with an error body
		// would corrupt the response. Match the committed-response behavior
		// of the other adapters and leave it untouched.
		return nil
	}
	if errHandler != nil {
		errHandler(fc, err)
		return nil
	}
	return err
}

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
