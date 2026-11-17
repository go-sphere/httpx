package hertzx

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"net/http"
	"net/textproto"
	"os"
	"path"
	"slices"
	"strconv"
	"strings"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol"
	"github.com/cloudwego/hertz/pkg/route"
	"github.com/go-sphere/httpx"
)

var _ httpx.Router = (*Router)(nil)

// Router is the hertz route scope returned by [Engine.Group] and
// [Router.Group], wrapping a *route.RouterGroup. Its zero value is not usable.
// Register routes before the engine starts serving.
type Router struct {
	group      *route.RouterGroup
	errHandler ErrorHandler
	// chain is composed into each route at registration instead of occupying a
	// hertz handler slot, so httpx middleware runs inside everything registered
	// through UseNative. It references the engine's chain rather than copying
	// it; see httpx.MiddlewareChain.
	chain       *httpx.MiddlewareChain
	maxBodySize int64
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

// BasePath returns this scope's absolute prefix as hertz reports it.
func (r *Router) BasePath() string {
	return r.group.BasePath()
}

// SupportsRouterFeature reports httpx.RouterFeatureNamedWildcard as
// supported: hertz matches /files/*filepath natively.
func (r *Router) SupportsRouterFeature(feature httpx.RouterFeature) bool {
	switch feature {
	case httpx.RouterFeatureNamedWildcard:
		return true
	default:
		return false
	}
}

// Group returns a nested scope under prefix with m registered on it as if by
// Use.
func (r *Router) Group(prefix string, m ...httpx.Middleware) httpx.Router {
	sub := r.chain.Sub()
	sub.Use(m...)
	return &Router{
		group:       r.group.Group(prefix),
		errHandler:  r.errHandler,
		chain:       sub,
		maxBodySize: r.maxBodySize,
	}
}

// Handle registers h for method (upper-cased) and path on the hertz group,
// wrapped in the httpx middleware registered so far on this scope and its
// parents. It panics on an invalid wildcard (httpx.ValidateWildcardPath) and
// on any registration hertz itself rejects, such as a conflicting route.
func (r *Router) Handle(method, path string, h httpx.Handler) {
	r.mustValidWildcard(path)
	r.group.Handle(strings.ToUpper(method), path, r.toHertzHandler(h))
}

// HandleStd mounts a plain net/http handler, implementing httpx.StdHandlerMounter.
// It goes through Handle so middleware registered on this scope also wraps it.
func (r *Router) HandleStd(method, path string, h http.Handler) {
	r.Handle(method, path, stdLeaf(h))
}

// Any registers h for path with hertz's Any, so the method set beyond the
// standard ones is hertz's.
func (r *Router) Any(path string, h httpx.Handler) {
	r.mustValidWildcard(path)
	r.group.Any(path, r.toHertzHandler(h))
}

// mustValidWildcard fails registration loudly and uniformly across adapters
// for wildcard shapes the shared contract does not support. Both relPath and
// the joined group+route pattern are validated.
func (r *Router) mustValidWildcard(relPath string) {
	if err := httpx.ValidateWildcardPath(relPath); err != nil {
		panic(err)
	}
	if err := httpx.ValidateWildcardPath(path.Join(r.group.BasePath(), relPath)); err != nil {
		panic(err)
	}
}

// Static serves the directory root at prefix; it is StaticFS with
// os.DirFS(root).
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
		err := checkBodySize(rc, r.maxBodySize)
		if err == nil {
			err = h(hc)
		}
		if err != nil {
			_ = rc.Error(err)
			// Skip the error handler when the response is already committed
			// (or the chain aborted) so a partial response is not corrupted
			// by a second body.
			if !rc.IsAborted() && !hertzResponseCommitted(rc) {
				// hc.Context, so a value a layer added with SetContext reaches
				// the error handler.
				r.errHandler(hc.Context(), rc, err)
				commitErrorStatus(rc, err)
			}
			if !rc.IsAborted() {
				rc.Abort()
			}
		}
	}
}

// checkBodySize refuses a request body over n bytes (n <= 0: no limit) with
// the *http.MaxBytesError the net/http based adapters produce. hertz has
// already buffered the body, so its length is known; a streamed body is judged
// by its declared Content-Length, since measuring it would read it.
func checkBodySize(rc *app.RequestContext, n int64) error {
	if n <= 0 {
		return nil
	}
	size := int64(rc.Request.Header.ContentLength())
	if !rc.Request.IsBodyStream() {
		size = int64(len(rc.Request.Body()))
	}
	if size > n {
		return &http.MaxBytesError{Limit: n}
	}
	return nil
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
	if bodyCommitsResponse(rc) {
		return true
	}
	committed, ok := rc.Get(responseCommittedKey)
	if !ok {
		return false
	}
	flag, _ := committed.(bool)
	return flag
}

// bodyCommitsResponse reports whether the response body alone shows the
// response committed: it is streaming or not empty.
func bodyCommitsResponse(rc *app.RequestContext) bool {
	return rc.Response.GetHijackWriter() != nil || rc.Response.IsBodyStream() || len(rc.Response.Body()) > 0
}

// markCommitted records a write that committed the response. The flag is only
// stored when the body cannot show it, since storing it allocates hertz's key
// map and takes its lock.
func markCommitted(rc *app.RequestContext) {
	if !bodyCommitsResponse(rc) {
		rc.Set(responseCommittedKey, true)
	}
}

// responseCommittedKey records explicit empty or streaming responses, which
// cannot reliably be distinguished from a bare Status by inspecting the body.
const responseCommittedKey = "httpx.hertzx.responseCommitted"

// explicitContentType returns the Content-Type set on h, or an empty slice
// when nothing set one: hertz's ContentType reports a default then, which it
// only sends with a non-empty body. The slice aliases h.
func explicitContentType(h *protocol.ResponseHeader) []byte {
	if h.NoDefaultContentType() {
		return h.ContentType()
	}
	h.SetNoDefaultContentType(true)
	ct := h.ContentType()
	h.SetNoDefaultContentType(false)
	return ct
}

// visitResponseHeader calls f with the canonical key and value of each header
// set on resp, as hertz would send it: the default Content-Type only with a
// non-empty body. Content-Length is left out; hertz derives it from the body
// when it writes the response.
func visitResponseHeader(resp *protocol.Response, f func(key, value string)) {
	resp.Header.VisitAll(func(k, v []byte) {
		key := textproto.CanonicalMIMEHeaderKey(string(k))
		if key == "Content-Type" || key == "Content-Length" {
			return
		}
		f(key, string(v))
	})
	ct := explicitContentType(&resp.Header)
	if len(ct) == 0 && (resp.IsBodyStream() || len(resp.Body()) > 0) {
		ct = resp.Header.ContentType()
	}
	if len(ct) > 0 {
		f("Content-Type", string(ct))
	}
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
		w := newStdResponseWriter(rc)
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

// stdResponseWriter is the http.ResponseWriter HandleStd and
// AdaptStdMiddleware hand to net/http code. It writes into hertz's buffered
// response under net/http's rules: Header starts as the headers the response
// carries, and the first write commits the changes made to it, replacing
// hertz's values for the keys it changed. On a response committed before the
// writer existed, status and headers are dropped and bytes append. It
// implements http.Flusher through the adapter's Flush.
type stdResponseWriter struct {
	rc     *app.RequestContext
	header http.Header
	// base is header as last read from hertz's response; what differs from it
	// is what net/http code changed.
	base        http.Header
	wroteHeader bool
	// late is set when the response was committed before the writer existed.
	late bool
	// streaming is set once the response streams past this writer; later
	// writes are dropped rather than appended to the stream.
	streaming bool
}

func newStdResponseWriter(rc *app.RequestContext) *stdResponseWriter {
	w := &stdResponseWriter{
		rc:        rc,
		header:    make(http.Header),
		base:      make(http.Header),
		late:      hertzResponseCommitted(rc),
		streaming: rc.Response.GetHijackWriter() != nil || rc.Response.IsBodyStream(),
	}
	w.mergeHertzHeader()
	return w
}

// mergeHertzHeader brings the headers hertz's response gained, changed or lost
// since base into both header and base, so the net/http side sees them and
// they do not count as its own changes. A key hertz left as it was keeps the
// value net/http code gave it.
func (w *stdResponseWriter) mergeHertzHeader() {
	current := make(http.Header)
	visitResponseHeader(&w.rc.Response, current.Add)
	for key, values := range current {
		if !slices.Equal(values, w.base[key]) {
			w.header[key] = values
			w.base[key] = slices.Clone(values)
		}
	}
	for key := range w.base {
		if _, ok := current[key]; !ok {
			delete(w.header, key)
			delete(w.base, key)
		}
	}
}

func (w *stdResponseWriter) Header() http.Header {
	return w.header
}

func (w *stdResponseWriter) pendingStatus() int {
	if s := w.rc.Response.StatusCode(); s > 0 {
		return s
	}
	return http.StatusOK
}

func (w *stdResponseWriter) WriteHeader(code int) {
	if w.wroteHeader {
		return
	}
	if code >= 100 && code < 200 && code != http.StatusSwitchingProtocols {
		return
	}
	w.wroteHeader = true
	if w.late || w.streaming {
		return
	}
	w.applyHeader()
	w.rc.Response.SetStatusCode(code)
	w.rc.Set(responseCommittedKey, true)
}

// applyHeader applies the changes made to header since base to hertz's
// response without committing it: a changed key replaces hertz's values and a
// deleted one is removed. On a response committed before the writer existed
// it does nothing.
func (w *stdResponseWriter) applyHeader() {
	if w.late {
		return
	}
	resp := &w.rc.Response
	for key := range w.base {
		if _, ok := w.header[key]; !ok {
			resp.Header.Del(key)
		}
	}
	for key, values := range w.header {
		if slices.Equal(values, w.base[key]) {
			continue
		}
		if strings.EqualFold(key, "Content-Length") {
			if len(values) > 0 {
				if n, err := strconv.Atoi(values[0]); err == nil {
					resp.Header.SetContentLength(n)
				}
			}
			continue
		}
		resp.Header.Del(key)
		for _, value := range values {
			resp.Header.Add(key, value)
		}
	}
}

func (w *stdResponseWriter) Write(b []byte) (int, error) {
	if w.streaming {
		return len(b), nil
	}
	if !w.wroteHeader {
		w.WriteHeader(w.pendingStatus())
	}
	if bodylessStatus(w.rc.Response.StatusCode()) {
		return len(b), nil
	}
	w.rc.Response.AppendBody(b)
	return len(b), nil
}

// Flush implements http.Flusher: it commits the response and flushes it as
// the adapter's Flush does, a no-op without a live connection.
func (w *stdResponseWriter) Flush() {
	if !w.wroteHeader {
		w.WriteHeader(w.pendingStatus())
	}
	// The stream this flush starts goes through w, so w keeps writing.
	streaming := w.streaming
	_ = flushResponse(w.rc)
	w.streaming = streaming
}
