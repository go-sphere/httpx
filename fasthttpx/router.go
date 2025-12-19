package fasthttpx

import (
	"io/fs"
	"path"
	"strings"
	"time"

	"github.com/go-sphere/httpx"
	"github.com/valyala/fasthttp"
)

var _ httpx.Router = (*Router)(nil)

type route struct {
	method  string
	path    string
	handler httpx.Handler
}

type Router struct {
	basePath    string
	routes      []route
	middlewares []httpx.Middleware
	engine      *Engine
}

func (r *Router) Use(m ...httpx.Middleware) {
	r.middlewares = append(r.middlewares, m...)
}

func (r *Router) BasePath() string {
	return r.basePath
}

func (r *Router) Group(prefix string, m ...httpx.Middleware) httpx.Router {
	return &Router{
		basePath:    joinPaths(r.basePath, prefix),
		routes:      make([]route, 0),
		middlewares: cloneMiddlewares(r.middlewares, m...),
		engine:      r.engine,
	}
}

func (r *Router) Handle(method, path string, h httpx.Handler) {
	fullPath := joinPaths(r.basePath, path)
	r.routes = append(r.routes, route{
		method:  strings.ToUpper(method),
		path:    fullPath,
		handler: h,
	})
	if r.engine != nil {
		r.engine.addRoute(strings.ToUpper(method), fullPath, h, r.middlewares)
	}
}

func (r *Router) Any(path string, h httpx.Handler) {
	methods := []string{"GET", "POST", "PUT", "DELETE", "PATCH", "HEAD", "OPTIONS"}
	for _, method := range methods {
		r.Handle(method, path, h)
	}
}

func (r *Router) Static(prefix, root string) {
	fullPrefix := joinPaths(r.basePath, prefix)
	fs := &fasthttp.FS{
		Root: root,
	}
	handler := fs.NewRequestHandler()
	if r.engine != nil {
		r.engine.server.Handler = func(ctx *fasthttp.RequestCtx) {
			path := string(ctx.Path())
			if strings.HasPrefix(path, fullPrefix) {
				handler(ctx)
				return
			}
			r.engine.defaultHandler(ctx)
		}
	}
}

func (r *Router) StaticFS(prefix string, filesystem fs.FS) {
	fullPrefix := joinPaths(r.basePath, prefix)
	handler := func(ctx *fasthttp.RequestCtx) {
		path := string(ctx.Path())
		if strings.HasPrefix(path, fullPrefix) {
			path = strings.TrimPrefix(path, fullPrefix)
		}
		if path == "" {
			path = "/"
		}
		
		file, err := filesystem.Open(strings.TrimPrefix(path, "/"))
		if err != nil {
			ctx.SetStatusCode(fasthttp.StatusNotFound)
			return
		}
		defer file.Close()
		
		// For FastHTTP, we need to handle file serving differently
		// This is a simplified implementation
		if stat, err := file.Stat(); err == nil {
			ctx.Response.Header.Set("Content-Type", "application/octet-stream")
			ctx.Response.Header.Set("Last-Modified", stat.ModTime().UTC().Format(time.RFC1123))
			ctx.Response.SetBodyStream(file, int(stat.Size()))
		}
	}
	// Add to engine's static handlers
	if r.engine != nil {
		r.engine.addStaticHandler(fullPrefix, handler)
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



func joinPaths(absolutePath, relativePath string) string {
	if relativePath == "" {
		return absolutePath
	}
	finalPath := path.Join(absolutePath, relativePath)
	appendSlash := lastChar(relativePath) == '/' && lastChar(finalPath) != '/'
	if appendSlash {
		return finalPath + "/"
	}
	return finalPath
}

func lastChar(str string) uint8 {
	if str == "" {
		panic("The length of the string can't be 0")
	}
	return str[len(str)-1]
}

func cloneMiddlewares(base []httpx.Middleware, additional ...httpx.Middleware) []httpx.Middleware {
	result := make([]httpx.Middleware, len(base)+len(additional))
	copy(result, base)
	copy(result[len(base):], additional)
	return result
}