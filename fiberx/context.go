package fiberx

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"time"

	"github.com/go-sphere/httpx"
	"github.com/gofiber/fiber/v3"
)

var _ httpx.Context = (*fiberContext)(nil)

type contextKeyType int

const contextAbortKey contextKeyType = 1

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
	return c.ctx.Path()
}

func (c *fiberContext) FullPath() string {
	return c.ctx.FullPath()
}

func (c *fiberContext) ClientIP() string {
	return c.ctx.IP()
}

func (c *fiberContext) Param(key string) string {
	return c.ctx.Params(key)
}

func (c *fiberContext) Params() map[string]string {
	route := c.ctx.Route()
	if route == nil || len(route.Params) == 0 {
		return nil
	}
	params := make(map[string]string, len(route.Params))
	for _, name := range route.Params {
		params[name] = c.ctx.Params(name)
	}
	return params
}

func (c *fiberContext) Query(key string) string {
	return c.ctx.Query(key)
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
	return c.ctx.Get(key)
}

func (c *fiberContext) Headers() map[string][]string {
	src := c.ctx.GetReqHeaders()
	if len(src) == 0 {
		return nil
	}
	out := make(map[string][]string, len(src))
	for k, v := range src {
		ck := textproto.CanonicalMIMEHeaderKey(k)
		out[ck] = append([]string(nil), v...)
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
	return c.ctx.FormValue(key)
}

func (c *fiberContext) MultipartForm() (*multipart.Form, error) {
	return c.ctx.MultipartForm()
}

func (c *fiberContext) FormFile(name string) (*multipart.FileHeader, error) {
	return c.ctx.FormFile(name)
}

func (c *fiberContext) BodyRaw() ([]byte, error) {
	return c.ctx.BodyRaw(), nil
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
	return c.ctx.Bind().JSON(dst)
}

func (c *fiberContext) BindQuery(dst any) error {
	return c.ctx.Bind().Query(dst)
}

func (c *fiberContext) BindForm(dst any) error {
	return c.ctx.Bind().Form(dst)
}

func (c *fiberContext) BindURI(dst any) error {
	return c.ctx.Bind().URI(dst)
}

func (c *fiberContext) BindHeader(dst any) error {
	return c.ctx.Bind().Header(dst)
}

// Responder (httpx.Responder)

func (c *fiberContext) Status(code int) {
	c.ctx.Status(code)
}

func (c *fiberContext) JSON(code int, v any) {
	_ = c.ctx.Status(code).JSON(v)
}

func (c *fiberContext) Text(code int, s string) {
	_ = c.ctx.Status(code).SendString(s)
}

func (c *fiberContext) NoContent(code int) {
	c.ctx.Status(code)
	c.ctx.Response().ResetBody()
}

func (c *fiberContext) Bytes(code int, b []byte, contentType string) {
	if contentType != "" {
		c.ctx.Set(fiber.HeaderContentType, contentType)
	}
	_ = c.ctx.Status(code).Send(b)
}

func (c *fiberContext) DataFromReader(code int, contentType string, r io.Reader, size int) {
	if contentType != "" {
		c.ctx.Set(fiber.HeaderContentType, contentType)
	}
	_ = c.ctx.Status(code).SendStream(r, size)
}

func (c *fiberContext) File(path string) {
	_ = c.ctx.SendFile(path)
}

func (c *fiberContext) Redirect(code int, location string) {
	redirect := c.ctx.Redirect()
	if code > 0 {
		redirect.Status(code)
	}
	_ = redirect.To(location)
}

func (c *fiberContext) SetHeader(key, value string) {
	c.ctx.Set(key, value)
}

func (c *fiberContext) SetCookie(cookie *http.Cookie) {
	if cookie == nil {
		return
	}
	if s := cookie.String(); s != "" {
		c.ctx.Response().Header.Add(fiber.HeaderSetCookie, s)
	}
}

// StateStore (httpx.StateStore)

func (c *fiberContext) Set(key string, val any) {
	c.ctx.Locals(key, val)
}

func (c *fiberContext) Get(key string) (any, bool) {
	val := c.ctx.Locals(key)
	if val == nil {
		return nil, false
	}
	return val, true
}

// Aborter (httpx.Aborter)

func (c *fiberContext) Abort() {
	Abort(c.ctx)
}

func (c *fiberContext) IsAborted() bool {
	return IsAborted(c.ctx)
}

// Context (context.Context + Next)

func (c *fiberContext) Deadline() (deadline time.Time, ok bool) {
	return c.ctx.RequestCtx().Deadline()
}

func (c *fiberContext) Done() <-chan struct{} {
	return c.ctx.RequestCtx().Done()
}

func (c *fiberContext) Err() error {
	return c.ctx.RequestCtx().Err()
}

func (c *fiberContext) Value(key any) any {
	if keyString, ok := key.(string); ok {
		if val, exists := c.Get(keyString); exists {
			return val
		}
	}
	return c.ctx.RequestCtx().Value(key)
}

func (c *fiberContext) Next() {
	if c.IsAborted() {
		return
	}
	_ = c.ctx.Next()
}

func IsAborted(ctx fiber.Ctx) bool {
	value := ctx.Locals(contextAbortKey)
	if value == nil {
		return false
	}
	flag, ok := value.(bool)
	return ok && flag
}

func Abort(ctx fiber.Ctx) {
	ctx.Locals(contextAbortKey, true)
}
