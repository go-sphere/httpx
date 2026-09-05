package hertzx

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strings"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/go-sphere/httpx"
)

var _ httpx.Context = (*hertzContext)(nil)

type hertzContext struct {
	ctx        *app.RequestContext
	baseCtx    context.Context
	nextCalled bool
}

func newHertzContext(ctx context.Context, rc *app.RequestContext) *hertzContext {
	return &hertzContext{
		ctx:     rc,
		baseCtx: ctx,
	}
}

// FromHertz wraps a hertz request context as httpx.Context. Use it from a
// hertz ErrorHandler to write through httpx helpers.
func FromHertz(ctx context.Context, rc *app.RequestContext) httpx.Context {
	return newHertzContext(ctx, rc)
}

// Request (httpx.Request)

func (c *hertzContext) Method() string {
	return string(c.ctx.Method())
}

func (c *hertzContext) Path() string {
	return string(c.ctx.Request.Path())
}

func (c *hertzContext) FullPath() string {
	return c.ctx.FullPath()
}

func (c *hertzContext) ClientIP() string {
	return c.ctx.ClientIP()
}

func (c *hertzContext) Param(key string) string {
	return c.normalizeParam(key, c.ctx.Param(key))
}

func (c *hertzContext) Params() map[string]string {
	if len(c.ctx.Params) == 0 {
		return nil
	}
	out := make(map[string]string, len(c.ctx.Params))
	for _, p := range c.ctx.Params {
		out[p.Key] = c.normalizeParam(p.Key, p.Value)
	}
	return out
}

// normalizeParam strips the leading "/" that hertz includes in wildcard
// values, so /files/*filepath yields "a/b" for /files/a/b on every adapter.
func (c *hertzContext) normalizeParam(key, value string) string {
	if strings.HasPrefix(value, "/") && strings.Contains(c.ctx.FullPath(), "*"+key) {
		return strings.TrimPrefix(value, "/")
	}
	return value
}

func (c *hertzContext) Query(key string) string {
	return c.ctx.Query(key)
}

func (c *hertzContext) Queries() map[string][]string {
	args := c.ctx.QueryArgs()
	if args.Len() == 0 {
		return nil
	}
	out := make(map[string][]string, args.Len())
	args.VisitAll(func(k, v []byte) {
		key := string(k)
		out[key] = append(out[key], string(v))
	})
	return out
}

func (c *hertzContext) RawQuery() string {
	return string(c.ctx.Request.QueryString())
}

func (c *hertzContext) Header(key string) string {
	return string(c.ctx.GetHeader(key))
}

func (c *hertzContext) Headers() map[string][]string {
	header := &c.ctx.Request.Header
	if header.Len() == 0 {
		return nil
	}
	out := make(map[string][]string, header.Len())
	header.VisitAll(func(k, v []byte) {
		key := textproto.CanonicalMIMEHeaderKey(string(k))
		if key == "Host" {
			// net/http moves Host out of the header map, so gin/echo never
			// expose it here; filter it for cross-adapter consistency.
			return
		}
		out[key] = append(out[key], string(v))
	})
	if len(out) == 0 {
		return nil
	}
	return out
}

func (c *hertzContext) Cookie(name string) (string, error) {
	val := c.ctx.Cookie(name)
	if val == nil {
		return "", http.ErrNoCookie
	}
	return string(val), nil
}

func (c *hertzContext) Cookies() map[string]string {
	header := &c.ctx.Request.Header
	if header.Len() == 0 {
		return nil
	}
	out := make(map[string]string)
	header.VisitAllCookie(func(k, v []byte) {
		out[string(k)] = string(v)
	})
	if len(out) == 0 {
		return nil
	}
	return out
}

func (c *hertzContext) FormValue(key string) string {
	return string(c.ctx.FormValue(key))
}

func (c *hertzContext) MultipartForm() (*multipart.Form, error) {
	return c.ctx.MultipartForm()
}

func (c *hertzContext) FormFile(name string) (*multipart.FileHeader, error) {
	return c.ctx.FormFile(name)
}

func (c *hertzContext) BodyRaw() ([]byte, error) {
	return c.ctx.Request.BodyE()
}

func (c *hertzContext) BodyReader() io.ReadCloser {
	if stream := c.ctx.Request.BodyStream(); stream != nil {
		return httpx.NewReadCloser(stream, c.ctx.Request.CloseBodyStream)
	}
	body := c.ctx.Request.Body()
	if len(body) == 0 {
		return http.NoBody
	}
	return httpx.NewReadCloser(bytes.NewReader(body), nil)
}

// Binder (httpx.Binder)

func (c *hertzContext) BindJSON(dst any) error {
	if err := c.ctx.BindJSON(dst); err != nil {
		return httpx.WrapBindError(err)
	}
	return httpx.WrapBindError(validateStruct(dst))
}

func (c *hertzContext) BindQuery(dst any) error {
	if err := c.ctx.BindQuery(dst); err != nil {
		return httpx.WrapBindError(err)
	}
	return httpx.WrapBindError(validateStruct(dst))
}

func (c *hertzContext) BindForm(dst any) error {
	if err := c.ctx.BindForm(dst); err != nil {
		return httpx.WrapBindError(err)
	}
	return httpx.WrapBindError(validateStruct(dst))
}

func (c *hertzContext) BindURI(dst any) error {
	if err := bindURIWithForm(dst, c.ctx); err != nil {
		return httpx.WrapBindError(err)
	}
	return httpx.WrapBindError(validateStruct(dst))
}

func (c *hertzContext) BindHeader(dst any) error {
	if err := c.ctx.BindHeader(dst); err != nil {
		return httpx.WrapBindError(err)
	}
	return httpx.WrapBindError(validateStruct(dst))
}

// Responder (httpx.Responder)

func (c *hertzContext) Status(code int) {
	c.ctx.Status(code)
}

func (c *hertzContext) JSON(code int, v any) error {
	// Marshal directly so an encoding failure is returned as an error
	// instead of panicking inside hertz's render.
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	c.ctx.Data(code, "application/json; charset=utf-8", b)
	return nil
}

func (c *hertzContext) Text(code int, s string) error {
	c.ctx.String(code, s)
	return nil
}

func (c *hertzContext) NoContent(code int) error {
	c.ctx.Status(code)
	c.ctx.Response.ResetBody()
	return nil
}

func (c *hertzContext) Bytes(code int, b []byte, contentType string) error {
	if contentType == "" {
		contentType = http.DetectContentType(b)
	}
	c.ctx.Data(code, contentType, b)
	return nil
}

func (c *hertzContext) DataFromReader(code int, contentType string, r io.Reader, size int) error {
	if contentType != "" {
		c.ctx.SetContentType(contentType)
	}
	c.ctx.Status(code)
	c.ctx.SetBodyStream(r, size)
	return nil
}

func (c *hertzContext) File(path string) error {
	c.ctx.File(path)
	return nil
}

func (c *hertzContext) Redirect(code int, location string) error {
	if !httpx.ValidRedirectCode(code) {
		return httpx.NewInternalServerError(fmt.Sprintf("cannot redirect with status code %d", code))
	}
	c.ctx.Redirect(code, []byte(location))
	return nil
}

func (c *hertzContext) SetHeader(key, value string) {
	c.ctx.Header(key, value)
}

func (c *hertzContext) SetCookie(cookie *http.Cookie) {
	if cookie == nil {
		return
	}
	// Serialize via net/http for full fidelity (Expires, Partitioned, no
	// value re-encoding), matching the other adapters. Note that Add appends,
	// so setting the same cookie name twice emits two Set-Cookie headers.
	if v := cookie.String(); v != "" {
		c.ctx.Response.Header.Add("Set-Cookie", v)
	}
}

// StateStore (httpx.StateStore)

func (c *hertzContext) Set(key string, val any) {
	c.ctx.Set(key, val)
}

func (c *hertzContext) Get(key string) (any, bool) {
	// A stored nil is reported as absent so all adapters agree (echo/fiber
	// cannot distinguish nil from missing).
	val, ok := c.ctx.Get(key)
	if !ok || val == nil {
		return nil, false
	}
	return val, true
}

// Context (context.Context accessor + Next)

func (c *hertzContext) Context() context.Context {
	return c.baseCtx
}

func (c *hertzContext) SetContext(ctx context.Context) {
	c.baseCtx = ctx
}

func (c *hertzContext) Next() error {
	c.nextCalled = true
	before := len(c.ctx.Errors)
	c.ctx.Next(c.baseCtx)

	if len(c.ctx.Errors) <= before {
		return nil
	}

	errList := make([]error, 0, len(c.ctx.Errors)-before)
	for _, err := range c.ctx.Errors[before:] {
		if err != nil {
			errList = append(errList, err.Err)
		}
	}

	return joinErrors(errList)
}

func joinErrors(errs []error) error {
	switch len(errs) {
	case 0:
		return nil
	case 1:
		return errs[0]
	default:
		return errors.Join(errs...)
	}
}

func (c *hertzContext) StatusCode() int {
	return c.ctx.Response.StatusCode()
}

func (c *hertzContext) NativeContext() any {
	return c.ctx
}
