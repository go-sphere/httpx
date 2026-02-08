package ginx

import (
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strings"
	"time"

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
	return c.ctx.Param(key)
}

func (c *ginContext) Params() map[string]string {
	if len(c.ctx.Params) == 0 {
		return nil
	}
	m := make(map[string]string, len(c.ctx.Params))
	for _, p := range c.ctx.Params {
		m[p.Key] = p.Value
	}
	return m
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
	value, err := c.ctx.Cookie(name)
	if err != nil {
		return "", http.ErrNoCookie
	}
	return value, nil
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
	if err := c.ctx.Request.ParseMultipartForm(32 << 20); err != nil {
		return nil, err
	}
	return c.ctx.Request.MultipartForm, nil
}

func (c *ginContext) FormFile(name string) (*multipart.FileHeader, error) {
	return c.ctx.FormFile(name)
}

func (c *ginContext) BodyRaw() ([]byte, error) {
	return c.ctx.GetRawData()
}

func (c *ginContext) BodyReader() io.ReadCloser {
	if c.ctx.Request.Body != nil {
		return c.ctx.Request.Body
	}
	return http.NoBody
}

// Binder (httpx.Binder)

func (c *ginContext) BindJSON(dst any) error {
	return c.ctx.ShouldBindJSON(dst)
}

func (c *ginContext) BindQuery(dst any) error {
	return queryBinding.Bind(c.ctx.Request, dst)
}

func (c *ginContext) BindForm(dst any) error {
	contentType := c.ctx.GetHeader("Content-Type")
	if strings.HasPrefix(strings.ToLower(contentType), "multipart/") {
		return c.ctx.ShouldBindWith(dst, binding.FormMultipart)
	}
	return c.ctx.ShouldBindWith(dst, binding.Form)
}

func (c *ginContext) BindURI(dst any) error {
	return c.ctx.ShouldBindUri(dst)
}

func (c *ginContext) BindHeader(dst any) error {
	return c.ctx.ShouldBindHeader(dst)
}

// Responder (httpx.Responder)

func (c *ginContext) Status(code int) {
	c.ctx.Status(code)
}

func (c *ginContext) JSON(code int, v any) error {
	c.ctx.JSON(code, v)
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
	c.ctx.DataFromReader(code, int64(size), contentType, r, nil)
	return nil
}

func (c *ginContext) File(path string) error {
	c.ctx.File(path)
	return nil
}

func (c *ginContext) Redirect(code int, location string) error {
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

// Context (context.Context + Next)

func (c *ginContext) Deadline() (deadline time.Time, ok bool) {
	return c.ctx.Deadline()
}

func (c *ginContext) Done() <-chan struct{} {
	return c.ctx.Done()
}

func (c *ginContext) Err() error {
	return c.ctx.Err()
}

func (c *ginContext) Value(key any) any {
	return c.ctx.Value(key)
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
