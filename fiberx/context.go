package fiberx

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"strings"

	"github.com/go-sphere/httpx"
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/binder"
)

var (
	_ httpx.Context = fiberContext[*fiber.DefaultCtx]{}
	_ httpx.Context = fiberContext[fiber.Ctx]{}
)

// The default context is a pointer, so this value fits directly into an
// interface without a heap-allocated wrapper. Custom Fiber contexts retain
// their concrete behavior through the interface-backed fallback.
type fiberContext[T fiber.Ctx] struct {
	ctx T
}

func newFiberContext(ctx fiber.Ctx) httpx.Context {
	if native, ok := ctx.(*fiber.DefaultCtx); ok {
		return fiberContext[*fiber.DefaultCtx]{ctx: native}
	}
	return &fiberContext[fiber.Ctx]{ctx: ctx}
}

// FromFiber wraps a fiber.Ctx as httpx.Context. Use it from a fiber
// ErrorHandler or native middleware to write through httpx helpers.
//
// It takes the fiber.Ctx interface and returns httpx.Context rather than
// exposing the generic fiberContext[T]. The type parameter exists only to let
// the common *fiber.DefaultCtx case fit in an interface without a heap wrapper;
// the two instantiations that exist are the two this package checks against
// httpx.Context, and a generic FromFiber[T fiber.Ctx] would let callers
// instantiate a third, unverified one.
//
// On a request this adapter did not route it cannot resolve the adapter's
// named-wildcard normalization, since the mapping is recorded on the request by
// the route the adapter registered: for a route registered natively as /files/*,
// FullPath reports "/files/*", Param("filepath") and BindURI's uri:"filepath"
// are empty, and Params carries the value under "*". Param("*") reaches the
// value on any route. A route this adapter registered keeps resolving normally,
// from whatever layer FromFiber is called.
func FromFiber(ctx fiber.Ctx) httpx.Context {
	return newFiberContext(ctx)
}

// Request (httpx.Request)

func (c fiberContext[T]) Method() string {
	return c.ctx.Method()
}

func (c fiberContext[T]) Path() string {
	// fiber's Path() keeps percent-encoding; fasthttp's URI().Path() returns
	// the decoded path, matching the other adapters. string() copies out of
	// the pooled buffer.
	return string(c.ctx.Request().URI().Path())
}

// unmatchedRouteKey names the Locals slot marking a request whose path no
// route matched. Fiber reports the engine's fallback middleware — registered
// with Use, so it lives at "/" — as the matched route, and FullPath would
// otherwise hand a layer that "/" for a 404, which a selector keyed on the
// pattern (authorization, rate limiting) would read as a route.
type unmatchedRouteKey struct{}

func markUnmatchedRoute(native fiber.Ctx) { native.Locals(unmatchedRouteKey{}, true) }

func unmatchedRoute(native fiber.Ctx) bool {
	unmatched, _ := native.Locals(unmatchedRouteKey{}).(bool)
	return unmatched
}

// FullPath reports the route pattern as it was registered. When the pattern
// carried a named wildcard, FixWildcardPathIfNeed rewrote it to fiber's
// anonymous form ("/files/*filepath" -> "/files/*") because fiber has no named
// wildcards — that rewrite is this adapter's business and must not surface
// here, or a caller matching on FullPath (downstream auth and rate limiting do)
// would see a different pattern than it registered. The trailing-byte check
// keeps a route without a wildcard from paying the map lookup.
//
// A path no route matched reports no pattern at all, like the other adapters;
// see unmatchedRouteKey.
func (c fiberContext[T]) FullPath() string {
	if unmatchedRoute(c.ctx) {
		return ""
	}
	pattern := c.ctx.FullPath()
	if lastCharIs('*', pattern) {
		if route := wildcardRouteOf(c.ctx); route != nil {
			return route.pattern
		}
	}
	return pattern
}

func (c fiberContext[T]) ClientIP() string {
	return c.ctx.IP()
}

func (c fiberContext[T]) Param(key string) string {
	v := c.paramValue(c.ctx.Params(key))
	if v == "" && key != "*" {
		// Named wildcard rewritten to "*" at registration time.
		if c.wildcardParamName() == key {
			return c.paramValue(c.ctx.Params("*"))
		}
	}
	return v
}

// wildcardParamName reports the name the matched route's wildcard was
// registered with, or "" when the route has no named wildcard. fiber only knows
// the parameter as "*" (spelled "*1" in Route().Params); every reading of the
// parameter set (Param, Params, BindURI) resolves it back through here so none
// of them can disagree.
func (c fiberContext[T]) wildcardParamName() string {
	if route := wildcardRouteOf(c.ctx); route != nil {
		return route.param
	}
	return ""
}

// paramValue makes a route parameter read the same as on gin, echo and hertz.
//
// The value is cloned because fiber (with the default Immutable=false) returns
// strings aliased to pooled fasthttp buffers. It is also decoded when the app
// runs with fiber's default UnescapePath=false: fiber then matches the raw
// path, so the parameter would arrive as "a%20b" where the other adapters hand
// back "a b". An engine built by this adapter sets UnescapePath, but one passed
// through WithEngine keeps its own config and cannot be changed afterwards.
//
// Both checks are ordered so an ordinary request pays nothing beyond the byte
// scan: a value with no '%' cannot need decoding, and only then is the app's
// config (a large struct copy) consulted.
func (c fiberContext[T]) paramValue(v string) string {
	v = strings.Clone(v)
	if !strings.ContainsRune(v, '%') || c.ctx.App().Config().UnescapePath {
		return v
	}
	decoded, err := url.PathUnescape(v)
	if err != nil {
		// A malformed escape is not an escape; hand back what was matched.
		return v
	}
	return decoded
}

// decodesParams reports whether route parameters of *this* request still carry
// percent-escapes the other adapters would have decoded.
func (c fiberContext[T]) decodesParams() bool {
	if !bytes.ContainsRune(c.ctx.Request().URI().PathOriginal(), '%') {
		return false
	}
	return !c.ctx.App().Config().UnescapePath
}

func (c fiberContext[T]) Params() map[string]string {
	route := c.ctx.Route()
	if route == nil || len(route.Params) == 0 {
		return nil
	}
	origName := c.wildcardParamName()
	params := make(map[string]string, len(route.Params))
	for _, name := range route.Params {
		value := c.paramValue(c.ctx.Params(name))
		// Fiber aliases "*" to "*1" internally; expose the canonical "*" key.
		key := name
		switch key {
		case "*1":
			key = "*"
		case "+1":
			key = "+"
		}
		if key == "*" && origName != "" {
			// "*" is this adapter's normalization artifact: the route was
			// registered as /*name, so Params must read the same as on an
			// adapter with native named wildcards. An anonymous /* route has
			// no recorded name and keeps its "*" key.
			params[origName] = value
			continue
		}
		params[key] = value
	}
	return params
}

func (c fiberContext[T]) Query(key string) string {
	return strings.Clone(c.ctx.Query(key))
}

func (c fiberContext[T]) Queries() map[string][]string {
	args := c.ctx.Request().URI().QueryArgs()
	if args.Len() == 0 {
		return nil
	}
	out := make(map[string][]string, args.Len())
	for keyBytes, valueBytes := range args.All() {
		key := string(keyBytes)
		out[key] = append(out[key], string(valueBytes))
	}
	return out
}

func (c fiberContext[T]) RawQuery() string {
	return string(c.ctx.Request().URI().QueryString())
}

func (c fiberContext[T]) Header(key string) string {
	// Host travels outside the header map on net/http, so the other four
	// adapters report it as unset. Headers() already skips it for that reason;
	// fasthttp keeps it in the header set, so Header has to skip it too instead
	// of being the one adapter where Header("Host") answers. Path(), Method()
	// and the request URI are how the host is meant to be reached.
	if strings.EqualFold(key, fiber.HeaderHost) {
		return ""
	}
	return strings.Clone(c.ctx.Get(key))
}

func (c fiberContext[T]) Headers() map[string][]string {
	src := c.ctx.GetReqHeaders()
	if len(src) == 0 {
		return nil
	}
	out := make(map[string][]string, len(src))
	for k, v := range src {
		// GetReqHeaders builds keys and values with unsafe strings aliased to
		// pooled fasthttp buffers (default Immutable=false); clone the key
		// before canonicalizing (CanonicalMIMEHeaderKey returns already
		// canonical input as-is) and clone every value.
		ck := textproto.CanonicalMIMEHeaderKey(strings.Clone(k))
		if ck == "Host" {
			continue
		}
		values := make([]string, len(v))
		for i, value := range v {
			values[i] = strings.Clone(value)
		}
		out[ck] = values
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func (c fiberContext[T]) Cookie(name string) (string, error) {
	value := c.ctx.Request().Header.Cookie(name)
	if value == nil {
		return "", http.ErrNoCookie
	}
	return string(value), nil
}

func (c fiberContext[T]) Cookies() map[string]string {
	out := make(map[string]string)
	for k, v := range c.ctx.Request().Header.Cookies() {
		out[string(k)] = string(v)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func (c fiberContext[T]) FormValue(key string) string {
	return strings.Clone(c.ctx.FormValue(key))
}

func (c fiberContext[T]) MultipartForm() (*multipart.Form, error) {
	return c.ctx.MultipartForm()
}

func (c fiberContext[T]) FormFile(name string) (*multipart.FileHeader, error) {
	return c.ctx.FormFile(name)
}

func (c fiberContext[T]) BodyRaw() ([]byte, error) {
	// Copy out of fasthttp's pooled buffer so the bytes stay valid after the
	// request completes.
	return bytes.Clone(c.ctx.BodyRaw()), nil
}

func (c fiberContext[T]) BodyReader() io.ReadCloser {
	if stream := c.ctx.Request().BodyStream(); stream != nil {
		return httpx.NewReadCloser(stream, c.ctx.Request().CloseBodyStream)
	}
	body := c.ctx.Body()
	if len(body) == 0 {
		return http.NoBody
	}
	return httpx.NewReadCloser(bytes.NewReader(body), nil)
}

// Binder (httpx.Binder)

func (c fiberContext[T]) BindJSON(dst any) error {
	return httpx.WrapBindError(c.ctx.Bind().JSON(dst))
}

func (c fiberContext[T]) BindQuery(dst any) error {
	return httpx.WrapBindError(c.ctx.Bind().Query(dst))
}

func (c fiberContext[T]) BindForm(dst any) error {
	return httpx.WrapBindError(c.ctx.Bind().Form(dst))
}

func (c fiberContext[T]) BindURI(dst any) error {
	return httpx.WrapBindError(c.bindURI(dst))
}

// bindURI runs fiber's own URI binder, but over this adapter's view of the
// parameter set rather than fiber's. Two things differ from fiber's own
// Bind().URI, and both would otherwise make BindURI contradict Param:
//
//   - a named wildcard reaches fiber as "*" ("*1" in Route().Params), so a field
//     tagged uri:"filepath" on a /files/*filepath route would match nothing and
//     bind "" — silently, since an absent parameter is not an error;
//   - values arrive percent-encoded when the app left UnescapePath off, so a
//     generated handler would bind "a%20b" where the other adapters bind "a b".
//
// Same binder, same tags, same conversion rules; only the names and the source
// of the strings differ. A route with neither problem keeps fiber's own path.
func (c fiberContext[T]) bindURI(dst any) error {
	route := c.ctx.Route()
	if route == nil || len(route.Params) == 0 {
		return nil
	}
	wildcard := c.wildcardParamName()
	if wildcard == "" && !c.decodesParams() {
		return c.ctx.Bind().URI(dst)
	}
	names := route.Params
	if wildcard != "" {
		// Route().Params belongs to the route, not the request: rename into a
		// copy.
		names = make([]string, len(route.Params))
		copy(names, route.Params)
		for i, name := range names {
			if name == "*" || name == "*1" {
				names[i] = wildcard
			}
		}
	}
	// URIBinding is stateless, so it needs neither fiber's pool nor a reset.
	return (&binder.URIBinding{}).Bind(names, func(key string, defaultValue ...string) string {
		if wildcard != "" && key == wildcard {
			key = "*"
		}
		if v := c.paramValue(c.ctx.Params(key)); v != "" {
			return v
		}
		if len(defaultValue) > 0 {
			return defaultValue[0]
		}
		return ""
	}, dst)
}

func (c fiberContext[T]) BindHeader(dst any) error {
	return httpx.WrapBindError(c.bindHeader(dst))
}

// bindHeader runs fiber's own header binder, but over this adapter's view of
// the request headers rather than fasthttp's — the same relationship bindURI
// has to fiber's URI binder, and for the same reason: the two readings of one
// header must not disagree.
//
// Host travels outside the header map on net/http, so the other four adapters
// have no Host header to bind at all, and Header/Headers here already skip it
// (see Header). fasthttp keeps it in the header set, so fiber's binder would
// fill a field tagged header:"Host", leaving BindHeader answering where Header
// on the same context does not.
//
// fasthttp yields Host from the header iteration only while it is non-empty, so
// emptying it for the duration of the bind is what takes it out of the binder's
// input. It is restored before anything else can read it, panic included, and
// only this goroutine is serving the request. Same binder, same tags, same
// conversion rules; only the header set differs. A request that carries no Host
// takes fiber's own path untouched.
func (c fiberContext[T]) bindHeader(dst any) error {
	header := &c.ctx.Request().Header
	host := header.Host()
	if len(host) == 0 {
		return c.ctx.Bind().Header(dst)
	}
	// The returned slice aliases the buffer that emptying the host reuses.
	saved := bytes.Clone(host)
	header.SetHostBytes(nil)
	defer header.SetHostBytes(saved)
	return c.ctx.Bind().Header(dst)
}

// Responder (httpx.Responder)

func (c fiberContext[T]) Status(code int) {
	if !responseDecided(c.ctx) {
		c.ctx.Status(code)
	}
}

func (c fiberContext[T]) JSON(code int, v any) error {
	return c.ctx.Status(code).JSON(v)
}

func (c fiberContext[T]) Text(code int, s string) error {
	err := c.ctx.Status(code).SendString(s)
	if err == nil && s == "" {
		markResponseCommitted(c.ctx)
	}
	return err
}

func (c fiberContext[T]) NoContent(code int) error {
	c.ctx.Status(code)
	c.ctx.Response().ResetBody()
	markResponseCommitted(c.ctx)
	return nil
}

func (c fiberContext[T]) Bytes(code int, b []byte, contentType string) error {
	if contentType == "" {
		contentType = http.DetectContentType(b)
	}
	c.ctx.Set(fiber.HeaderContentType, contentType)
	err := c.ctx.Status(code).Send(b)
	if err == nil && len(b) == 0 {
		markResponseCommitted(c.ctx)
	}
	return err
}

func (c fiberContext[T]) DataFromReader(code int, contentType string, r io.Reader, size int64) error {
	if contentType != "" {
		c.ctx.Set(fiber.HeaderContentType, contentType)
	}
	return c.ctx.Status(code).SendStream(r, streamSize(size))
}

// streamSize narrows the contract's int64 size to the int that fiber's
// SendStream takes. A length that does not fit in int (only reachable on a
// 32-bit build) degrades to -1 — unknown size, chunked transfer — because the
// alternative, a truncating conversion, would advertise a Content-Length that
// does not match the body and corrupt the response. The body itself is still
// streamed in full either way.
func streamSize(size int64) int {
	if size < 0 || int64(int(size)) != size {
		return -1
	}
	return int(size)
}

func (c fiberContext[T]) File(path string) error {
	return c.ctx.SendFile(path)
}

func (c fiberContext[T]) Redirect(code int, location string) error {
	if !httpx.ValidRedirectCode(code) {
		return httpx.NewInternalServerError(fmt.Sprintf("cannot redirect with status code %d", code))
	}
	err := c.ctx.Redirect().Status(code).To(location)
	if err == nil {
		markResponseCommitted(c.ctx)
	}
	return err
}

func (c fiberContext[T]) SetHeader(key, value string) {
	c.ctx.Set(key, value)
}

func (c fiberContext[T]) SetCookie(cookie *http.Cookie) {
	if cookie != nil {
		if s := cookie.String(); s != "" {
			c.ctx.Response().Header.Add(fiber.HeaderSetCookie, s)
		}
	}
}

// StateStore (httpx.StateStore)

// Set and Get follow StateStore's request-local, externally synchronized
// contract. A package-level lock would serialize unrelated requests without
// protecting native Locals calls made outside this adapter.
func (c fiberContext[T]) Set(key string, val any) {
	c.ctx.Locals(key, val)
}

func (c fiberContext[T]) Get(key string) (any, bool) {
	val := c.ctx.Locals(key)
	if val == nil {
		return nil, false
	}
	return val, true
}

// Context (context.Context accessor)

func (c fiberContext[T]) Context() context.Context {
	return c.ctx.Context()
}

func (c fiberContext[T]) SetContext(ctx context.Context) {
	c.ctx.SetContext(ctx)
}

func (c fiberContext[T]) StatusCode() int {
	return c.ctx.Response().StatusCode()
}

func (c fiberContext[T]) NativeContext() any {
	return c.ctx
}

// Stream implements httpx.Streamer. Fiber's response model is buffered, so
// fn runs after the handler returns: Stream returns nil immediately and fn's
// error only terminates the stream. Each write inside fn is flushed to the
// client immediately.
//
// fiberContext intentionally does not implement httpx.Flusher: fiber cannot
// flush mid-handler; probe with httpx.AsFlusher and fall back, or use Stream.
func (c fiberContext[T]) Stream(code int, contentType string, fn func(w io.Writer) error) error {
	if contentType != "" {
		c.ctx.Set(fiber.HeaderContentType, contentType)
	}
	c.ctx.Status(code)
	return c.ctx.SendStreamWriter(func(w *bufio.Writer) {
		_ = fn(bufioFlushWriter{w: w})
	})
}

type bufioFlushWriter struct {
	w *bufio.Writer
}

func (fw bufioFlushWriter) Write(p []byte) (int, error) {
	n, err := fw.w.Write(p)
	if err != nil {
		return n, err
	}
	if err := fw.w.Flush(); err != nil {
		return n, err
	}
	return n, nil
}
