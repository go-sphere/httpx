package fasthttpx

import (
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"

	"github.com/go-sphere/httpx"
	"github.com/valyala/fasthttp"
)

var _ httpx.Context = (*Context)(nil)

type Context struct {
	ctx         *fasthttp.RequestCtx
	middlewares []httpx.Middleware
	handler     httpx.Handler
	index       int
	aborted     bool
	state       map[string]interface{}
}

func newFastHTTPContext(ctx *fasthttp.RequestCtx, middlewares []httpx.Middleware) *Context {
	return &Context{
		ctx:         ctx,
		middlewares: middlewares,
		index:       -1,
		state:       make(map[string]interface{}),
	}
}

// Context methods
func (c *Context) Deadline() (deadline time.Time, ok bool) {
	return time.Time{}, false
}

func (c *Context) Done() <-chan struct{} {
	return nil
}

func (c *Context) Err() error {
	return nil
}

func (c *Context) Value(key interface{}) interface{} {
	if keyStr, ok := key.(string); ok {
		return c.state[keyStr]
	}
	return nil
}

// Request methods
func (c *Context) Method() string {
	return string(c.ctx.Method())
}

func (c *Context) Path() string {
	return string(c.ctx.Path())
}

func (c *Context) FullPath() string {
	// FastHTTP doesn't have route patterns, return empty
	return ""
}

func (c *Context) ClientIP() string {
	return c.ctx.RemoteIP().String()
}

func (c *Context) Param(key string) string {
	return c.ctx.UserValue(key).(string)
}

func (c *Context) Params() map[string]string {
	// FastHTTP doesn't provide a way to get all params
	return nil
}

func (c *Context) Query(key string) string {
	return string(c.ctx.QueryArgs().Peek(key))
}

func (c *Context) Queries() map[string][]string {
	result := make(map[string][]string)
	c.ctx.QueryArgs().VisitAll(func(key, value []byte) {
		k := string(key)
		v := string(value)
		result[k] = append(result[k], v)
	})
	if len(result) == 0 {
		return nil
	}
	return result
}

func (c *Context) RawQuery() string {
	return string(c.ctx.QueryArgs().QueryString())
}

func (c *Context) Header(key string) string {
	return string(c.ctx.Request.Header.Peek(key))
}

func (c *Context) Headers() map[string][]string {
	result := make(map[string][]string)
	c.ctx.Request.Header.VisitAll(func(key, value []byte) {
		k := string(key)
		v := string(value)
		result[k] = append(result[k], v)
	})
	return result
}

func (c *Context) Cookie(name string) (string, error) {
	value := string(c.ctx.Request.Header.Cookie(name))
	if value == "" {
		return "", http.ErrNoCookie
	}
	return value, nil
}

func (c *Context) Cookies() map[string]string {
	result := make(map[string]string)
	c.ctx.Request.Header.VisitAllCookie(func(key, value []byte) {
		result[string(key)] = string(value)
	})
	if len(result) == 0 {
		return nil
	}
	return result
}

func (c *Context) FormValue(key string) string {
	return string(c.ctx.PostArgs().Peek(key))
}

func (c *Context) MultipartForm() (*multipart.Form, error) {
	return c.ctx.MultipartForm()
}

func (c *Context) FormFile(name string) (*multipart.FileHeader, error) {
	return c.ctx.FormFile(name)
}

func (c *Context) BodyRaw() ([]byte, error) {
	return c.ctx.PostBody(), nil
}

func (c *Context) BodyReader() io.ReadCloser {
	return io.NopCloser(strings.NewReader(string(c.ctx.PostBody())))
}

// Responder methods
func (c *Context) Status(code int) {
	c.ctx.SetStatusCode(code)
}

func (c *Context) JSON(code int, v interface{}) {
	c.ctx.SetStatusCode(code)
	c.ctx.SetContentType("application/json")
	// Simplified implementation - in real code, use json.Marshal
	c.ctx.SetBodyString("{}")
}

func (c *Context) Text(code int, s string) {
	c.ctx.SetStatusCode(code)
	c.ctx.SetContentType("text/plain; charset=utf-8")
	c.ctx.SetBodyString(s)
}

func (c *Context) NoContent(code int) {
	c.ctx.SetStatusCode(code)
}

func (c *Context) Bytes(code int, b []byte, contentType string) {
	c.ctx.SetStatusCode(code)
	c.ctx.SetContentType(contentType)
	c.ctx.SetBody(b)
}

func (c *Context) DataFromReader(code int, contentType string, r io.Reader, size int) {
	c.ctx.SetStatusCode(code)
	c.ctx.SetContentType(contentType)
	c.ctx.SetBodyStream(r, size)
}

func (c *Context) File(path string) {
	fasthttp.ServeFile(c.ctx, path)
}

func (c *Context) Redirect(code int, location string) {
	c.ctx.Redirect(location, code)
}

func (c *Context) SetHeader(key, value string) {
	c.ctx.Response.Header.Set(key, value)
}

func (c *Context) SetCookie(cookie *http.Cookie) {
	fc := &fasthttp.Cookie{}
	fc.SetKey(cookie.Name)
	fc.SetValue(cookie.Value)
	fc.SetMaxAge(cookie.MaxAge)
	fc.SetPath(cookie.Path)
	fc.SetDomain(cookie.Domain)
	fc.SetSecure(cookie.Secure)
	fc.SetHTTPOnly(cookie.HttpOnly)
	c.ctx.Response.Header.SetCookie(fc)
}

// Binder methods
func (c *Context) BindJSON(dst interface{}) error {
	// Simplified implementation
	return nil
}

func (c *Context) BindQuery(dst interface{}) error {
	// Simplified implementation
	return nil
}

func (c *Context) BindForm(dst interface{}) error {
	// Simplified implementation
	return nil
}

func (c *Context) BindURI(dst interface{}) error {
	// Simplified implementation
	return nil
}

func (c *Context) BindHeader(dst interface{}) error {
	// Simplified implementation
	return nil
}

// StateStore methods
func (c *Context) Set(key string, value interface{}) {
	c.state[key] = value
}

func (c *Context) Get(key string) (interface{}, bool) {
	value, exists := c.state[key]
	return value, exists
}

func (c *Context) MustGet(key string) interface{} {
	if value, exists := c.state[key]; exists {
		return value
	}
	panic("Key \"" + key + "\" does not exist")
}

// Aborter methods
func (c *Context) Abort() {
	c.aborted = true
}

func (c *Context) IsAborted() bool {
	return c.aborted
}

// Middleware chain
func (c *Context) Next() {
	if c.aborted {
		return
	}
	c.index++
	for c.index < len(c.middlewares) && !c.aborted {
		c.middlewares[c.index](c)
		c.index++
	}
	// After all middleware, call the handler
	if !c.aborted && c.handler != nil && c.index >= len(c.middlewares) {
		c.handler(c)
	}
}