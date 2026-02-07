package hertzx

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol"
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
	return c.ctx.Param(key)
}

func (c *hertzContext) Params() map[string]string {
	if len(c.ctx.Params) == 0 {
		return nil
	}
	out := make(map[string]string, len(c.ctx.Params))
	for _, p := range c.ctx.Params {
		out[p.Key] = p.Value
	}
	return out
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
		out[key] = append(out[key], string(v))
	})
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
	return c.ctx.BindJSON(dst)
}

func (c *hertzContext) BindQuery(dst any) error {
	return c.ctx.BindQuery(dst)
}

func (c *hertzContext) BindForm(dst any) error {
	return c.ctx.BindForm(dst)
}

func (c *hertzContext) BindURI(dst any) error {
	return bindURIWithForm(dst, c.Params())
}

func (c *hertzContext) BindHeader(dst any) error {
	return c.ctx.BindHeader(dst)
}

// Responder (httpx.Responder)

func (c *hertzContext) Status(code int) {
	c.ctx.Status(code)
}

func (c *hertzContext) JSON(code int, v any) error {
	c.ctx.JSON(code, v)
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
	c.ctx.Redirect(code, []byte(location))
	return nil
}

func (c *hertzContext) SetHeader(key, value string) {
	c.ctx.Header(key, value)
}

func (c *hertzContext) SetCookie(cookie *http.Cookie) {
	if cookie != nil {
		c.ctx.SetCookie(
			cookie.Name,
			cookie.Value,
			cookie.MaxAge,
			cookie.Path,
			cookie.Domain,
			mapSameSite(cookie.SameSite),
			cookie.Secure,
			cookie.HttpOnly,
		)
	}
}

// StateStore (httpx.StateStore)

func (c *hertzContext) Set(key string, val any) {
	c.ctx.Set(key, val)
}

func (c *hertzContext) Get(key string) (any, bool) {
	return c.ctx.Get(key)
}

// Context (context.Context + Next)

func (c *hertzContext) Deadline() (deadline time.Time, ok bool) {
	return c.baseCtx.Deadline()
}

func (c *hertzContext) Done() <-chan struct{} {
	return c.baseCtx.Done()
}

func (c *hertzContext) Err() error {
	return c.baseCtx.Err()
}

func (c *hertzContext) Value(key any) any {
	if str, ok := key.(string); ok {
		if val, exist := c.Get(str); exist {
			return val
		}
	}
	return c.baseCtx.Value(key)
}

func (c *hertzContext) Next() error {
	c.nextCalled = true
	c.ctx.Next(c.baseCtx)

	if len(c.ctx.Errors) > 0 {
		// Return the most recent error (last in the slice)
		return c.ctx.Errors.Last()
	}

	return nil
}

func (c *hertzContext) StatusCode() int {
	return c.ctx.Response.StatusCode()
}

func (c *hertzContext) NativeContext() any {
	return c.ctx
}

func mapSameSite(mode http.SameSite) protocol.CookieSameSite {
	switch mode {
	case http.SameSiteStrictMode:
		return protocol.CookieSameSiteStrictMode
	case http.SameSiteNoneMode:
		return protocol.CookieSameSiteNoneMode
	case http.SameSiteDefaultMode:
		return protocol.CookieSameSiteDefaultMode
	default:
		return protocol.CookieSameSiteLaxMode
	}
}
