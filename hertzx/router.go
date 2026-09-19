package hertzx

import (
	"bytes"
	"context"
	"errors"
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
	// chain is composed into each route at registration instead of occupying a
	// hertz handler slot, so httpx middleware runs inside everything registered
	// through UseNative. It references the engine's chain rather than copying
	// it; see httpx.MiddlewareChain.
	chain *httpx.MiddlewareChain
}

// Use registers httpx middleware on this scope, implementing
// httpx.MiddlewareScope. The chain is composed into every route registered
// afterwards, so it needs no native handler slot and no per-request object; see
// httpx.Middleware and httpx.MiddlewareChain for the ordering rules.
func (r *Router) Use(m ...httpx.Middleware) {
	r.chain.Use(m...)
}

// UseNative registers native hertz middleware on this group, the only way to
// mount an app.HandlerFunc: it gets its own hertz handler slot, which an
// httpx.Middleware wrapper could not reproduce — all httpx layers share the
// route's single slot, so the wrapped handler's ctx.Next() would advance hertz
// past that slot rather than into the httpx chain.
func (r *Router) UseNative(handlers ...app.HandlerFunc) {
	r.group.Use(handlers...)
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
	sub := r.chain.Sub()
	sub.Use(m...)
	return &Router{
		group:      r.group.Group(prefix),
		errHandler: r.errHandler,
		chain:      sub,
	}
}

func (r *Router) Handle(method, path string, h httpx.Handler) {
	mustValidWildcard(path)
	r.group.Handle(strings.ToUpper(method), path, r.toHertzHandler(h))
}

// HandleStd mounts a plain net/http handler, implementing httpx.StdHandlerMounter.
// It goes through Handle so middleware registered on this scope also wraps it.
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
// ordinary route, so middleware on this scope wraps static requests too.
func (r *Router) StaticFS(prefix string, fsys fs.FS) {
	pattern := httpx.StaticRoutePattern(prefix)
	leaf := stdLeaf(httpx.StaticFileHandler(path.Join(r.group.BasePath(), prefix), fsys))
	r.Handle(http.MethodGet, pattern, leaf)
	r.Handle(http.MethodHead, pattern, leaf)
}

// stdLeaf serves a plain net/http handler through hertz's request context, so a
// std handler can sit at the end of a composed middleware chain.
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
	h = r.chain.Compose(h)
	return func(ctx context.Context, rc *app.RequestContext) {
		hc := newHertzContext(ctx, rc)
		if err := h(hc); err != nil {
			_ = rc.Error(err)
			// Skip the error handler when the response is already committed
			// (or the chain aborted) so a partial response is not corrupted
			// by a second body.
			if !rc.IsAborted() && !hertzResponseCommitted(rc) {
				r.errHandler(ctx, rc, err)
				commitErrorStatus(rc, err)
			}
			if !rc.IsAborted() {
				rc.Abort()
			}
		}
	}
}

// commitErrorStatus records the error's own status when the configured
// ErrorHandler rendered nothing for it.
//
// Rendering nothing is a legitimate shape — a handler that only logs, and
// leaves the body to a layer above — but the chain is aborted right after, and
// the status hertz has recorded is still the 200 every response starts at, so
// the request answered 200 with an empty body. The error's own status is the
// floor; a status the error handler set for itself still wins, and a response
// it wrote is never touched (the caller checks hertzResponseCommitted first).
// On hertz's own NoRoute/NoMethod path nothing changes, because hertz records
// 404/405 before running the fallback and the guard below already holds.
func commitErrorStatus(rc *app.RequestContext, err error) {
	if hertzResponseCommitted(rc) || rc.Response.StatusCode() != http.StatusOK {
		return
	}
	_, status, _ := httpx.ClassifyError(err)
	rc.SetStatusCode(int(status))
}

// hertzResponseCommitted reports whether the handler already produced output.
// A hijacked writer — installed by the Streamer/SSE path — takes over header and
// body writing, so Response.Body() stays empty while bytes have in fact already
// reached the client; checking only the buffer would let the error handler
// append a second body to a streaming response.
func hertzResponseCommitted(rc *app.RequestContext) bool {
	if rc.Response.GetHijackWriter() != nil || rc.Response.IsBodyStream() || len(rc.Response.Body()) > 0 {
		return true
	}
	committed, ok := rc.Get(responseCommittedKey)
	if !ok {
		return false
	}
	flag, _ := committed.(bool)
	return flag
}

// responseCommittedKey records explicit empty or streaming responses, which
// cannot reliably be distinguished from a bare Status by inspecting the body.
const responseCommittedKey = "httpx.hertzx.responseCommitted"

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
	w.rc.Set(responseCommittedKey, true)
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
