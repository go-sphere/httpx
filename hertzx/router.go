package hertzx

import (
	"bytes"
	"context"
	"io/fs"
	"net/http"
	"os"
	"path"
	"strconv"
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
	mustValidWildcard(path)
	r.group.Handle(strings.ToUpper(method), path, r.toHertzHandler(h))
}

// HandleStd mounts a plain net/http handler, implementing httpx.StdHandlerMounter.
func (r *Router) HandleStd(method, path string, h http.Handler) {
	mustValidWildcard(path)
	r.group.Handle(strings.ToUpper(method), path, toStdHandler(h))
}

func (r *Router) Any(path string, h httpx.Handler) {
	mustValidWildcard(path)
	r.group.Any(path, r.toHertzHandler(h))
}

// mustValidWildcard fails registration loudly and uniformly across adapters
// for wildcard shapes the shared contract does not support.
func mustValidWildcard(path string) {
	if err := httpx.ValidateWildcardPath(path); err != nil {
		panic(err)
	}
}

func (r *Router) Static(prefix, root string) {
	r.StaticFS(prefix, os.DirFS(root))
}

// StaticFS serves files through net/http's FileServer so Range requests,
// If-Modified-Since (304), and content sniffing behave like the gin/echo
// adapters instead of a hand-rolled sendfile loop. Directories (and the bare
// prefix) return 404 instead of a listing, matching the echo/fiber adapters.
func (r *Router) StaticFS(prefix string, fsys fs.FS) {
	urlPattern := path.Join(prefix, "/*filepath")
	strip := path.Join(r.group.BasePath(), prefix)
	handler := http.Handler(http.FileServer(http.FS(fsys)))
	if strip != "" && strip != "/" {
		handler = http.StripPrefix(strip, handler)
	}
	fileHandler := toStdHandler(handler)
	hertzHandler := func(ctx context.Context, rc *app.RequestContext) {
		name := strings.TrimPrefix(rc.Param("filepath"), "/")
		rel := strings.TrimPrefix(path.Clean("/"+name), "/")
		if rel == "" || rel == "." {
			rc.Status(http.StatusNotFound)
			return
		}
		if info, err := fs.Stat(fsys, rel); err != nil || info.IsDir() {
			rc.Status(http.StatusNotFound)
			return
		}
		fileHandler(ctx, rc)
	}
	r.group.GET(urlPattern, hertzHandler)
	r.group.HEAD(urlPattern, hertzHandler)
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

func (r *Router) toHertzHandler(h httpx.Handler) app.HandlerFunc {
	return func(ctx context.Context, rc *app.RequestContext) {
		hc := newHertzContext(ctx, rc)
		if err := h(hc); err != nil {
			_ = rc.Error(err)
			// Skip the error handler when the response is already committed
			// (or the chain aborted) so a partial response is not corrupted
			// by a second body.
			if !rc.IsAborted() && !hertzResponseCommitted(rc) {
				r.errHandler(ctx, rc, err)
			}
			if !rc.IsAborted() {
				rc.Abort()
			}
		}
	}
}

// hertzResponseCommitted reports whether the handler already produced a body.
func hertzResponseCommitted(rc *app.RequestContext) bool {
	return len(rc.Response.Body()) > 0
}

// toStdHandler bridges a net/http handler into hertz's buffered response,
// so it works both over the network and with in-process test dispatch.
func toStdHandler(h http.Handler) app.HandlerFunc {
	return func(ctx context.Context, rc *app.RequestContext) {
		req, err := compatRequest(ctx, rc)
		if err != nil {
			rc.AbortWithStatus(http.StatusInternalServerError)
			return
		}
		w := &stdResponseWriter{rc: rc}
		h.ServeHTTP(w, req)
		if !w.wroteHeader {
			w.WriteHeader(http.StatusOK)
		}
	}
}

// compatRequest builds a net/http request view of the hertz request.
func compatRequest(ctx context.Context, rc *app.RequestContext) (*http.Request, error) {
	body := rc.Request.Body()
	req, err := http.NewRequestWithContext(ctx, string(rc.Request.Method()), rc.Request.URI().String(), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	rc.Request.Header.VisitAll(func(k, v []byte) {
		key := string(k)
		if strings.EqualFold(key, "Host") {
			req.Host = string(v)
			return
		}
		req.Header.Add(key, string(v))
	})
	return req, nil
}

type stdResponseWriter struct {
	rc          *app.RequestContext
	header      http.Header
	wroteHeader bool
}

func (w *stdResponseWriter) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}

func (w *stdResponseWriter) WriteHeader(code int) {
	if w.wroteHeader {
		return
	}
	w.wroteHeader = true
	for key, values := range w.header {
		if strings.EqualFold(key, "Content-Length") {
			if len(values) > 0 {
				if n, err := strconv.Atoi(values[0]); err == nil {
					w.rc.Response.Header.SetContentLength(n)
				}
			}
			continue
		}
		for _, value := range values {
			w.rc.Response.Header.Add(key, value)
		}
	}
	w.rc.Response.SetStatusCode(code)
}

func (w *stdResponseWriter) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	w.rc.Response.AppendBody(b)
	return len(b), nil
}
