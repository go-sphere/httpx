package ginx

import (
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
	ctx *gin.Context
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
	return c.ctx.Request.URL.Query()
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
	return c.ctx.Cookie(name)
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

func (c *ginContext) JSON(code int, v any) {
	c.ctx.JSON(code, v)
}

func (c *ginContext) Text(code int, s string) {
	c.ctx.String(code, s)
}

func (c *ginContext) NoContent(code int) {
	c.ctx.Status(code)
}

func (c *ginContext) Bytes(code int, b []byte, contentType string) {
	c.ctx.Data(code, contentType, b)
}

func (c *ginContext) DataFromReader(code int, contentType string, r io.Reader, size int) {
	c.ctx.DataFromReader(code, int64(size), contentType, r, nil)
}

func (c *ginContext) File(path string) {
	c.ctx.File(path)
}

func (c *ginContext) Redirect(code int, location string) {
	c.ctx.Redirect(code, location)
}

func (c *ginContext) SetHeader(key, value string) {
	c.ctx.Header(key, value)
}

func (c *ginContext) SetCookie(cookie *http.Cookie) {
	if cookie == nil {
		return
	}
	http.SetCookie(c.ctx.Writer, cookie)
}

// StateStore (httpx.StateStore)

func (c *ginContext) Set(key string, val any) {
	c.ctx.Set(key, val)
}

func (c *ginContext) Get(key string) (any, bool) {
	return c.ctx.Get(key)
}

// Aborter (httpx.Aborter)

func (c *ginContext) Abort() {
	c.ctx.Abort()
}

func (c *ginContext) IsAborted() bool {
	return c.ctx.IsAborted()
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

func (c *ginContext) Next() {
	c.ctx.Next()
}
