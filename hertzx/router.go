package hertzx

import (
	"context"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/route"
	"github.com/go-sphere/httpx"
)

var _ httpx.Router = (*Router)(nil)

type Router struct {
	group      *route.RouterGroup
	errHandler ErrorHandler
}

func (r *Router) Use(m ...httpx.Middleware) {
	r.group.Use(adaptMiddlewares(m, r.errHandler)...)
}

func (r *Router) BasePath() string {
	return r.group.BasePath()
}

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
		group:      r.group.Group(prefix, adaptMiddlewares(m, r.errHandler)...),
		errHandler: r.errHandler,
	}
}

func (r *Router) Handle(method, path string, h httpx.Handler) {
	method = strings.ToUpper(method)
	r.group.Handle(method, path, r.toHertzHandler(h))
}

func (r *Router) Any(path string, h httpx.Handler) {
	r.group.Any(path, r.toHertzHandler(h))
}

func (r *Router) Static(prefix, root string) {
	r.StaticFS(prefix, osDirFS(root))
}

func (r *Router) StaticFS(prefix string, fs fs.FS) {
	urlPattern := path.Join(prefix, "/*filepath")
	handler := r.toStaticHandler(fs)
	r.group.GET(urlPattern, handler)
	r.group.HEAD(urlPattern, handler)
}

// GET registers a new GET route for a path with matching handler.
func (r *Router) GET(path string, h httpx.Handler) {
	r.group.GET(path, r.toHertzHandler(h))
}

// POST registers a new POST route for a path with matching handler.
func (r *Router) POST(path string, h httpx.Handler) {
	r.group.POST(path, r.toHertzHandler(h))
}

// PUT registers a new PUT route for a path with matching handler.
func (r *Router) PUT(path string, h httpx.Handler) {
	r.group.PUT(path, r.toHertzHandler(h))
}

// DELETE registers a new DELETE route for a path with matching handler.
func (r *Router) DELETE(path string, h httpx.Handler) {
	r.group.DELETE(path, r.toHertzHandler(h))
}

// PATCH registers a new PATCH route for a path with matching handler.
func (r *Router) PATCH(path string, h httpx.Handler) {
	r.group.PATCH(path, r.toHertzHandler(h))
}

// HEAD registers a new HEAD route for a path with matching handler.
func (r *Router) HEAD(path string, h httpx.Handler) {
	r.group.HEAD(path, r.toHertzHandler(h))
}

// OPTIONS registers a new OPTIONS route for a path with matching handler.
func (r *Router) OPTIONS(path string, h httpx.Handler) {
	r.group.OPTIONS(path, r.toHertzHandler(h))
}

func (r *Router) toHertzHandler(h httpx.Handler) app.HandlerFunc {
	return func(ctx context.Context, rc *app.RequestContext) {
		hc := newHertzContext(ctx, rc)
		if err := h(hc); err != nil {
			_ = rc.Error(err)
			r.errHandler(ctx, rc, err)
			if !rc.IsAborted() {
				rc.Abort()
			}
		}
	}
}

func (r *Router) toStaticHandler(files fs.FS) app.HandlerFunc {
	return func(ctx context.Context, rc *app.RequestContext) {
		name := strings.TrimPrefix(rc.Param("filepath"), "/")
		if name == "" || name == "." {
			rc.Status(http.StatusNotFound)
			return
		}

		clean := path.Clean("/" + name)
		if strings.Contains(clean, "..") {
			rc.Status(http.StatusNotFound)
			return
		}
		rel := strings.TrimPrefix(clean, "/")

		file, err := files.Open(rel)
		if err != nil {
			rc.Status(http.StatusNotFound)
			return
		}
		defer func() {
			_ = file.Close()
		}()

		info, err := file.Stat()
		if err != nil || info.IsDir() {
			rc.Status(http.StatusNotFound)
			return
		}

		contentType := mime.TypeByExtension(filepath.Ext(rel))
		if contentType == "" {
			contentType = "application/octet-stream"
		}

		rc.SetContentType(contentType)
		rc.Status(http.StatusOK)
		if string(rc.Method()) == http.MethodHead {
			return
		}

		body, err := io.ReadAll(file)
		if err != nil {
			rc.Status(http.StatusInternalServerError)
			return
		}
		rc.Response.SetBody(body)
	}
}

func osDirFS(root string) fs.FS {
	return os.DirFS(root)
}
