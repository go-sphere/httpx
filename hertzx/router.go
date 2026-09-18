package hertzx

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path"
	"slices"
	"strconv"
	"strings"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/route"
	"github.com/go-sphere/httpx"
)

var (
	_ httpx.Router           = (*Router)(nil)
	_ httpx.InterceptorScope = (*Router)(nil)
)

type Router struct {
	group        *route.RouterGroup
	errHandler   ErrorHandler
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
		group:        r.group.Group(prefix, adaptMiddlewares(m, r.errHandler)...),
		errHandler:   r.errHandler,
		interceptors: r.interceptors,
	}
}

func (r *Router) Handle(method, path string, h httpx.Handler) {
	mustValidWildcard(path)
	r.group.Handle(strings.ToUpper(method), path, r.toHertzHandler(h))
}

// HandleStd mounts a plain net/http handler, implementing httpx.StdHandlerMounter.
// It goes through Handle so interceptors registered on this scope also wrap it.
func (r *Router) HandleStd(method, path string, h http.Handler) {
	r.Handle(method, path, stdLeaf(h))
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

// StaticFS serves fsys through httpx.StaticFileHandler and registers it as an
// ordinary route, so interceptors on this scope wrap static requests too.
func (r *Router) StaticFS(prefix string, fsys fs.FS) {
	pattern := httpx.StaticRoutePattern(prefix)
	leaf := stdLeaf(httpx.StaticFileHandler(path.Join(r.group.BasePath(), prefix), fsys))
	r.Handle(http.MethodGet, pattern, leaf)
	r.Handle(http.MethodHead, pattern, leaf)
}

// stdLeaf serves a plain net/http handler through hertz's request context, so a
// std handler can sit at the end of a composed interceptor chain.
func stdLeaf(h http.Handler) httpx.Handler {
	return func(ctx httpx.Context) error {
		rc, ok := httpx.AsNativeContext[*app.RequestContext](ctx)
		if !ok {
			return errors.New("hertzx: hertz context type error")
		}
		toStdHandler(h)(ctx.Context(), rc)
		return nil
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

func (r *Router) toHertzHandler(h httpx.Handler) app.HandlerFunc {
	// Composed once per route, never per request.
	h = httpx.ComposeInterceptors(h, r.interceptors)
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

// hertzResponseCommitted reports whether the handler already produced output.
// A hijacked writer — installed by the Streamer/SSE path — takes over header
// and body writing, so Response.Body() stays empty while bytes have in fact
// already reached the client; checking only the buffer would let the error
// handler append a second body to a streaming response.
func hertzResponseCommitted(rc *app.RequestContext) bool {
	if rc.Response.GetHijackWriter() != nil || len(rc.Response.Body()) > 0 {
		return true
	}
	// A bodyless response is decided without writing any bytes, so "has a body"
	// cannot detect it: 204/304/1xx carry no body by definition, and a redirect
	// carries only a Location. A bare Status(code) is deliberately *not* counted —
	// it records a code without producing a response, and swallowing an error
	// behind it would turn a failure into a silent 2xx.
	status := rc.Response.StatusCode()
	if status == http.StatusNoContent || status == http.StatusNotModified || status < http.StatusOK {
		return true
	}
	if httpx.ValidRedirectCode(status) && len(rc.Response.Header.Peek("Location")) > 0 {
		return true
	}
	// A stream commits a 200 before the callback runs. Over a real connection
	// that installs a hijack writer, but in buffered dispatch nothing is
	// observable, so Stream records it explicitly.
	committed, ok := rc.Get(streamCommittedKey)
	if !ok {
		return false
	}
	flag, _ := committed.(bool)
	return flag
}

// streamCommittedKey marks a request whose response was committed by Stream.
// Set on the streaming path only, so the ordinary paths keep their allocation
// profile.
const streamCommittedKey = "httpx.hertzx.streamCommitted"

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
	// The peer address is not part of the hertz header set, so std handlers
	// reading r.RemoteAddr (ReverseProxy-style X-Forwarded-For, IP filters)
	// would otherwise always see an empty value on this adapter.
	req.RemoteAddr = rc.RemoteAddr().String()
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
