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

func (c fiberContext[T]) FullPath() string {
	return c.ctx.FullPath()
}

func (c fiberContext[T]) ClientIP() string {
	return c.ctx.IP()
}

func (c fiberContext[T]) Param(key string) string {
	v := c.paramValue(c.ctx.Params(key))
	if v == "" && key != "*" {
		// Named wildcard rewritten to "*" at registration time.
		if route := c.ctx.Route(); route != nil {
			if orig, ok := wildcardNames.Load(route.Path); ok && orig == key {
				return c.paramValue(c.ctx.Params("*"))
			}
		}
	}
	return v
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
	origName := ""
	if orig, ok := wildcardNames.Load(route.Path); ok {
		origName, _ = orig.(string)
	}
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
	if err := c.ctx.Bind().JSON(dst); err != nil {
		return httpx.WrapBindError(err)
	}
	return httpx.WrapBindError(validateStruct(dst))
}

func (c fiberContext[T]) BindQuery(dst any) error {
	if err := c.ctx.Bind().Query(dst); err != nil {
		return httpx.WrapBindError(err)
	}
	return httpx.WrapBindError(validateStruct(dst))
}

func (c fiberContext[T]) BindForm(dst any) error {
	if err := c.ctx.Bind().Form(dst); err != nil {
		return httpx.WrapBindError(err)
	}
	return httpx.WrapBindError(validateStruct(dst))
}

func (c fiberContext[T]) BindURI(dst any) error {
	if err := c.bindURI(dst); err != nil {
		return httpx.WrapBindError(err)
	}
	return httpx.WrapBindError(validateStruct(dst))
}

// bindURI runs fiber's own URI binder, but feeds it decoded values when the app
// left UnescapePath off — otherwise a generated handler would bind "a%20b"
// where the other adapters bind "a b". Same binder, same tags, same conversion
// rules; only the source of the strings differs.
func (c fiberContext[T]) bindURI(dst any) error {
	if !c.decodesParams() {
		return c.ctx.Bind().URI(dst)
	}
	route := c.ctx.Route()
	if route == nil || len(route.Params) == 0 {
		return nil
	}
	// URIBinding is stateless, so it needs neither fiber's pool nor a reset.
	return (&binder.URIBinding{}).Bind(route.Params, func(key string, defaultValue ...string) string {
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
	if err := c.ctx.Bind().Header(dst); err != nil {
		return httpx.WrapBindError(err)
	}
	return httpx.WrapBindError(validateStruct(dst))
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

func (c fiberContext[T]) DataFromReader(code int, contentType string, r io.Reader, size int) error {
	if contentType != "" {
		c.ctx.Set(fiber.HeaderContentType, contentType)
	}
	return c.ctx.Status(code).SendStream(r, size)
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

// Context (context.Context accessor + Next)

func (c fiberContext[T]) Context() context.Context {
	return c.ctx.Context()
}

func (c fiberContext[T]) SetContext(ctx context.Context) {
	c.ctx.SetContext(ctx)
}

func (c fiberContext[T]) Next() error {
	if err := c.ctx.Next(); err != nil {
		return err
	}
	// An inner layer's error is not returned through fiber once the adapter
	// has dealt with it (fiber would render it a second time, over a response
	// that is already written), so it is parked on the context; surface it
	// here to keep it visible to the layers above.
	return handledError(c.ctx)
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
