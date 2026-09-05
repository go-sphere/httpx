package echox

import (
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

// wildcardNames maps a registered route pattern (with the anonymous "*"
// wildcard) to the original named wildcard parameter, so Param(name) keeps
// working after FixWildcardPathIfNeed rewrote the path.
var wildcardNames sync.Map // route pattern -> original param name

type Router struct {
	group      *echo.Group
	basePath   string
	errHandler httpx.ErrorHandler
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
		group:      r.group.Group(prefix, adaptMiddlewares(m, r.errHandler)...),
		basePath:   joinPaths(r.basePath, prefix),
		errHandler: r.errHandler,
	}
}

// normalizeWildcardPath rewrites named wildcards (/*filepath) to echo's
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
	r.group.Add(strings.ToUpper(method), r.normalizeWildcardPath(path), r.toEchoHandler(h))
}

// HandleStd mounts a plain net/http handler, implementing httpx.StdHandlerMounter.
func (r *Router) HandleStd(method, path string, h http.Handler) {
	r.group.Add(strings.ToUpper(method), r.normalizeWildcardPath(path), echo.WrapHandler(h))
}

func (r *Router) Any(path string, h httpx.Handler) {
	r.group.Any(r.normalizeWildcardPath(path), r.toEchoHandler(h))
}

func (r *Router) Static(prefix, root string) {
	r.StaticFS(prefix, os.DirFS(root))
}

func (r *Router) StaticFS(prefix string, filesystem fs.FS) {
	// Register GET and HEAD on "<prefix>/*" (with a path separator) instead of
	// echo's native prefix+"*" route, which over-matches adjacent URLs
	// (/assetshello.txt) and rejects HEAD with 405.
	pattern := joinPaths(prefix, "/*")
	handler := echo.StaticDirectoryHandler(filesystem, false)
	r.group.GET(pattern, handler)
	r.group.HEAD(pattern, handler)
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
	return func(ec echo.Context) error {
		ctx := newEchoContext(ec)
		err := h(ctx)
		if err != nil {
			if r.errHandler != nil {
				if !ec.Response().Committed {
					r.errHandler(ctx, err)
				}
				return nil
			}
			return err
		}
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
