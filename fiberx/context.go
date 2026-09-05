package fiberx

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strings"
	"sync"

	"github.com/go-sphere/httpx"
	"github.com/gofiber/fiber/v3"
)

// stateMu guards StateStore access. Fiber's Locals is backed by fasthttp's
// unsynchronized userValues, unlike the other adapters which lock internally;
// the shared mutex keeps cross-adapter behavior consistent for middleware
// that reads state from short-lived goroutines.
var stateMu sync.RWMutex

var _ httpx.Context = (*fiberContext)(nil)

type fiberContext struct {
	ctx fiber.Ctx
}

func newFiberContext(ctx fiber.Ctx) *fiberContext {
	return &fiberContext{
		ctx: ctx,
	}
}

// Request (httpx.Request)

func (c *fiberContext) Method() string {
	return c.ctx.Method()
}

func (c *fiberContext) Path() string {
	return strings.Clone(c.ctx.Path())
}

func (c *fiberContext) FullPath() string {
	return c.ctx.FullPath()
}

func (c *fiberContext) ClientIP() string {
	return c.ctx.IP()
}

func (c *fiberContext) Param(key string) string {
	// Values are cloned because fiber (with the default Immutable=false)
	// returns strings aliased to pooled fasthttp buffers.
	v := strings.Clone(c.ctx.Params(key))
	if v == "" && key != "*" {
		// Named wildcard rewritten to "*" at registration time.
		if route := c.ctx.Route(); route != nil {
			if orig, ok := wildcardNames.Load(route.Path); ok && orig == key {
				return strings.Clone(c.ctx.Params("*"))
			}
		}
	}
	return v
}

func (c *fiberContext) Params() map[string]string {
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
		value := strings.Clone(c.ctx.Params(name))
		// Fiber aliases "*" to "*1" internally; expose the canonical "*" key.
		key := name
		switch key {
		case "*1":
			key = "*"
		case "+1":
			key = "+"
		}
		params[key] = value
		if key == "*" && origName != "" {
			params[origName] = value
		}
	}
	return params
}

func (c *fiberContext) Query(key string) string {
	return strings.Clone(c.ctx.Query(key))
}

func (c *fiberContext) Queries() map[string][]string {
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

func (c *fiberContext) RawQuery() string {
	return string(c.ctx.Request().URI().QueryString())
}

func (c *fiberContext) Header(key string) string {
	return strings.Clone(c.ctx.Get(key))
}

func (c *fiberContext) Headers() map[string][]string {
	src := c.ctx.GetReqHeaders()
	if len(src) == 0 {
		return nil
	}
	out := make(map[string][]string, len(src))
	for k, v := range src {
		ck := textproto.CanonicalMIMEHeaderKey(k)
		if ck == "Host" {
			continue
		}
		out[ck] = append([]string(nil), v...)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func (c *fiberContext) Cookie(name string) (string, error) {
	value := c.ctx.Request().Header.Cookie(name)
	if value == nil {
		return "", http.ErrNoCookie
	}
	return string(value), nil
}

func (c *fiberContext) Cookies() map[string]string {
	out := make(map[string]string)
	for k, v := range c.ctx.Request().Header.Cookies() {
		out[string(k)] = string(v)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func (c *fiberContext) FormValue(key string) string {
	return strings.Clone(c.ctx.FormValue(key))
}

func (c *fiberContext) MultipartForm() (*multipart.Form, error) {
	return c.ctx.MultipartForm()
}

func (c *fiberContext) FormFile(name string) (*multipart.FileHeader, error) {
	return c.ctx.FormFile(name)
}

func (c *fiberContext) BodyRaw() ([]byte, error) {
	// Copy out of fasthttp's pooled buffer so the bytes stay valid after the
	// request completes.
	return bytes.Clone(c.ctx.BodyRaw()), nil
}

func (c *fiberContext) BodyReader() io.ReadCloser {
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

func (c *fiberContext) BindJSON(dst any) error {
	return httpx.WrapBindError(c.ctx.Bind().JSON(dst))
}

func (c *fiberContext) BindQuery(dst any) error {
	return httpx.WrapBindError(c.ctx.Bind().Query(dst))
}

func (c *fiberContext) BindForm(dst any) error {
	return httpx.WrapBindError(c.ctx.Bind().Form(dst))
}

func (c *fiberContext) BindURI(dst any) error {
	return httpx.WrapBindError(c.ctx.Bind().URI(dst))
}

func (c *fiberContext) BindHeader(dst any) error {
	return httpx.WrapBindError(c.ctx.Bind().Header(dst))
}

// Responder (httpx.Responder)

func (c *fiberContext) Status(code int) {
	c.ctx.Status(code)
}

func (c *fiberContext) JSON(code int, v any) error {
	return c.ctx.Status(code).JSON(v)
}

func (c *fiberContext) Text(code int, s string) error {
	return c.ctx.Status(code).SendString(s)
}

func (c *fiberContext) NoContent(code int) error {
	c.ctx.Status(code)
	c.ctx.Response().ResetBody()
	return nil
}

func (c *fiberContext) Bytes(code int, b []byte, contentType string) error {
	if contentType != "" {
		c.ctx.Set(fiber.HeaderContentType, contentType)
	}
	return c.ctx.Status(code).Send(b)
}

func (c *fiberContext) DataFromReader(code int, contentType string, r io.Reader, size int) error {
	if contentType != "" {
		c.ctx.Set(fiber.HeaderContentType, contentType)
	}
	return c.ctx.Status(code).SendStream(r, size)
}

func (c *fiberContext) File(path string) error {
	return c.ctx.SendFile(path)
}

func (c *fiberContext) Redirect(code int, location string) error {
	if !httpx.ValidRedirectCode(code) {
		return httpx.NewInternalServerError(fmt.Sprintf("cannot redirect with status code %d", code))
	}
	return c.ctx.Redirect().Status(code).To(location)
}

func (c *fiberContext) SetHeader(key, value string) {
	c.ctx.Set(key, value)
}

func (c *fiberContext) SetCookie(cookie *http.Cookie) {
	if cookie != nil {
		if s := cookie.String(); s != "" {
			c.ctx.Response().Header.Add(fiber.HeaderSetCookie, s)
		}
	}
}

// StateStore (httpx.StateStore)

func (c *fiberContext) Set(key string, val any) {
	stateMu.Lock()
	defer stateMu.Unlock()
	c.ctx.Locals(key, val)
}

func (c *fiberContext) Get(key string) (any, bool) {
	stateMu.RLock()
	defer stateMu.RUnlock()
	val := c.ctx.Locals(key)
	if val == nil {
		return nil, false
	}
	return val, true
}

// Context (context.Context accessor + Next)

func (c *fiberContext) Context() context.Context {
	return c.ctx.Context()
}

func (c *fiberContext) SetContext(ctx context.Context) {
	c.ctx.SetContext(ctx)
}

func (c *fiberContext) Next() error {
	return c.ctx.Next()
}

func (c *fiberContext) StatusCode() int {
	return c.ctx.Response().StatusCode()
}

func (c *fiberContext) NativeContext() any {
	return c.ctx
}
