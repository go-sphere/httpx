package ginx

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

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/go-sphere/httpx"
)

var _ httpx.Context = (*ginContext)(nil)

var queryBinding = QueryBinding{}

type ginContext struct {
	ctx        *gin.Context
	nextCalled bool
}

func newGinContext(gc *gin.Context) *ginContext {
	return &ginContext{
		ctx: gc,
	}
}

// FromGin wraps a gin.Context as httpx.Context. Use it from a gin ErrorHandler
// to write through httpx helpers such as sphere/httpz.AbortWithJsonError.
func FromGin(gc *gin.Context) httpx.Context {
	return newGinContext(gc)
}

// Request (httpx.Request)

func (c *ginContext) Method() string {
	return c.ctx.Request.Method
}

func (c *ginContext) Path() string {
	return c.ctx.Request.URL.Path
}

func (c *ginContext) FullPath() string {
	return c.ctx.FullPath()
}

func (c *ginContext) ClientIP() string {
	return c.ctx.ClientIP()
}

func (c *ginContext) Param(key string) string {
	return c.normalizeParam(key, c.ctx.Param(key))
}

func (c *ginContext) Params() map[string]string {
	if len(c.ctx.Params) == 0 {
		return nil
	}
	m := make(map[string]string, len(c.ctx.Params))
	for _, p := range c.ctx.Params {
		m[p.Key] = c.normalizeParam(p.Key, p.Value)
	}
	return m
}

// normalizeParam strips the leading "/" that gin includes in wildcard values,
// so /files/*filepath yields "a/b" for /files/a/b on every adapter.
func (c *ginContext) normalizeParam(key, value string) string {
	if strings.HasPrefix(value, "/") && strings.Contains(c.ctx.FullPath(), "*"+key) {
		return strings.TrimPrefix(value, "/")
	}
	return value
}

func (c *ginContext) Query(key string) string {
	return c.ctx.Query(key)
}

func (c *ginContext) Queries() map[string][]string {
	queries := c.ctx.Request.URL.Query()
	if len(queries) == 0 {
		return nil
	}
	out := make(map[string][]string, len(queries))
	for k, v := range queries {
		out[k] = append([]string(nil), v...)
	}
	return out
}

func (c *ginContext) RawQuery() string {
	return c.ctx.Request.URL.RawQuery
}

func (c *ginContext) Header(key string) string {
	return c.ctx.GetHeader(key)
}

func (c *ginContext) Headers() map[string][]string {
	src := c.ctx.Request.Header
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

func (c *ginContext) Cookie(name string) (string, error) {
	// Read the raw cookie value (no query-unescaping) so Cookie and Cookies
	// agree with each other and with the other adapters.
	cookie, err := c.ctx.Request.Cookie(name)
	if err != nil {
		return "", http.ErrNoCookie
	}
	return cookie.Value, nil
}

func (c *ginContext) Cookies() map[string]string {
	raw := c.ctx.Request.Cookies()
	if len(raw) == 0 {
		return nil
	}
	out := make(map[string]string, len(raw))
	for _, cookie := range raw {
		out[cookie.Name] = cookie.Value
	}
	return out
}

func (c *ginContext) FormValue(key string) string {
	return c.ctx.Request.FormValue(key)
}

func (c *ginContext) MultipartForm() (*multipart.Form, error) {
	// Delegate to gin so the engine's configured MaxMultipartMemory is honored.
	return c.ctx.MultipartForm()
}

func (c *ginContext) FormFile(name string) (*multipart.FileHeader, error) {
	return c.ctx.FormFile(name)
}

func (c *ginContext) BodyRaw() ([]byte, error) {
	body, err := c.ctx.GetRawData()
	if err != nil {
		return nil, err
	}
	// Restore the body so subsequent Bind*/BodyReader calls still work.
	c.ctx.Request.Body = io.NopCloser(bytes.NewReader(body))
	return body, nil
}

func (c *ginContext) BodyReader() io.ReadCloser {
	if c.ctx.Request.Body != nil {
		return c.ctx.Request.Body
	}
	return http.NoBody
}

// Binder (httpx.Binder)

func (c *ginContext) BindJSON(dst any) error {
	return httpx.WrapBindError(c.ctx.ShouldBindJSON(dst))
}

func (c *ginContext) BindQuery(dst any) error {
	return httpx.WrapBindError(queryBinding.Bind(c.ctx.Request, dst))
}

func (c *ginContext) BindForm(dst any) error {
	contentType := c.ctx.GetHeader("Content-Type")
	if strings.HasPrefix(strings.ToLower(contentType), "multipart/") {
		return httpx.WrapBindError(c.ctx.ShouldBindWith(dst, binding.FormMultipart))
	}
	return httpx.WrapBindError(c.ctx.ShouldBindWith(dst, binding.Form))
}

func (c *ginContext) BindURI(dst any) error {
	return httpx.WrapBindError(c.ctx.ShouldBindUri(dst))
}

func (c *ginContext) BindHeader(dst any) error {
	return httpx.WrapBindError(c.ctx.ShouldBindHeader(dst))
}

// Responder (httpx.Responder)

func (c *ginContext) Status(code int) {
	c.ctx.Status(code)
}

func (c *ginContext) JSON(code int, v any) error {
	// Marshal directly so an encoding failure is returned as an error
	// instead of being buried in gin's c.Errors.
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	c.ctx.Data(code, "application/json; charset=utf-8", b)
	return nil
}

func (c *ginContext) Text(code int, s string) error {
	c.ctx.String(code, s)
	return nil
}

func (c *ginContext) NoContent(code int) error {
	c.ctx.Status(code)
	return nil
}

func (c *ginContext) Bytes(code int, b []byte, contentType string) error {
	c.ctx.Data(code, contentType, b)
	return nil
}

func (c *ginContext) DataFromReader(code int, contentType string, r io.Reader, size int) error {
	if rc, ok := r.(io.Closer); ok {
		defer func() {
			_ = rc.Close()
		}()
	}
	if size < 0 {
		if contentType != "" {
			c.ctx.Header("Content-Type", contentType)
		}
		c.ctx.Status(code)
		_, err := io.Copy(c.ctx.Writer, r)
		return err
	}
	c.ctx.DataFromReader(code, int64(size), contentType, r, nil)
	return nil
}

func (c *ginContext) File(path string) error {
	c.ctx.File(path)
	return nil
}

func (c *ginContext) Redirect(code int, location string) error {
	if !httpx.ValidRedirectCode(code) {
		return httpx.NewInternalServerError(fmt.Sprintf("cannot redirect with status code %d", code))
	}
	c.ctx.Redirect(code, location)
	return nil
}

func (c *ginContext) SetHeader(key, value string) {
	c.ctx.Header(key, value)
}

func (c *ginContext) SetCookie(cookie *http.Cookie) {
	if cookie != nil {
		http.SetCookie(c.ctx.Writer, cookie)
	}
}

// StateStore (httpx.StateStore)

func (c *ginContext) Set(key string, val any) {
	c.ctx.Set(key, val)
}

func (c *ginContext) Get(key string) (any, bool) {
	return c.ctx.Get(key)
}

// Context (context.Context accessor + Next)

func (c *ginContext) Context() context.Context {
	return c.ctx.Request.Context()
}

func (c *ginContext) SetContext(ctx context.Context) {
	c.ctx.Request = c.ctx.Request.WithContext(ctx)
}

func (c *ginContext) Next() error {
	c.nextCalled = true
	before := len(c.ctx.Errors)
	c.ctx.Next()

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

func (c *ginContext) StatusCode() int {
	return c.ctx.Writer.Status()
}

func (c *ginContext) NativeContext() any {
	return c.ctx
}
