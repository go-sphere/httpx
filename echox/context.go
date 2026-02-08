package echox

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strconv"
	"time"

	"github.com/go-sphere/httpx"
	"github.com/labstack/echo/v4"
)

var (
	_ httpx.Context = (*echoContext)(nil)
)

type echoContext struct {
	ctx    echo.Context
	next   echo.HandlerFunc
	binder echo.DefaultBinder
}

func newEchoContext(ctx echo.Context) *echoContext {
	return &echoContext{
		ctx: ctx,
	}
}

// Request (httpx.Request)

func (c *echoContext) Method() string {
	return c.ctx.Request().Method
}

func (c *echoContext) Path() string {
	return c.ctx.Request().URL.Path
}

func (c *echoContext) FullPath() string {
	return c.ctx.Path()
}

func (c *echoContext) ClientIP() string {
	return c.ctx.RealIP()
}

func (c *echoContext) Param(key string) string {
	return c.ctx.Param(key)
}

func (c *echoContext) Params() map[string]string {
	names := c.ctx.ParamNames()
	if len(names) == 0 {
		return nil
	}
	values := c.ctx.ParamValues()
	out := make(map[string]string, len(names))
	for i, name := range names {
		if i < len(values) {
			out[name] = values[i]
		} else {
			out[name] = ""
		}
	}
	return out
}

func (c *echoContext) Query(key string) string {
	return c.ctx.QueryParam(key)
}

func (c *echoContext) Queries() map[string][]string {
	values := c.ctx.QueryParams()
	if len(values) == 0 {
		return nil
	}
	out := make(map[string][]string, len(values))
	for k, v := range values {
		out[k] = append([]string(nil), v...)
	}
	return out
}

func (c *echoContext) RawQuery() string {
	return c.ctx.QueryString()
}

func (c *echoContext) Header(key string) string {
	return c.ctx.Request().Header.Get(key)
}

func (c *echoContext) Headers() map[string][]string {
	src := c.ctx.Request().Header
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

func (c *echoContext) Cookie(name string) (string, error) {
	cookie, err := c.ctx.Cookie(name)
	if err != nil {
		return "", http.ErrNoCookie
	}
	return cookie.Value, nil
}

func (c *echoContext) Cookies() map[string]string {
	raw := c.ctx.Cookies()
	if len(raw) == 0 {
		return nil
	}
	out := make(map[string]string, len(raw))
	for _, cookie := range raw {
		out[cookie.Name] = cookie.Value
	}
	return out
}

func (c *echoContext) FormValue(key string) string {
	return c.ctx.FormValue(key)
}

func (c *echoContext) MultipartForm() (*multipart.Form, error) {
	return c.ctx.MultipartForm()
}

func (c *echoContext) FormFile(name string) (*multipart.FileHeader, error) {
	return c.ctx.FormFile(name)
}

func (c *echoContext) BodyRaw() ([]byte, error) {
	req := c.ctx.Request()
	if req.Body == nil {
		return nil, nil
	}
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	_ = req.Body.Close()
	req.Body = io.NopCloser(bytes.NewReader(body))
	return body, nil
}

func (c *echoContext) BodyReader() io.ReadCloser {
	if body := c.ctx.Request().Body; body != nil {
		return body
	}
	return http.NoBody
}

// Request helpers not defined on httpx.Request but kept for compatibility.

// Binder (httpx.Binder)

func (c *echoContext) BindJSON(dst any) error {
	return c.binder.BindBody(c.ctx, dst)
}

func (c *echoContext) BindQuery(dst any) error {
	return c.binder.BindQueryParams(c.ctx, dst)
}

func (c *echoContext) BindForm(dst any) error {
	return c.binder.BindBody(c.ctx, dst)
}

func (c *echoContext) BindURI(dst any) error {
	return bindURIWithForm(dst, c.ctx)
}

func (c *echoContext) BindHeader(dst any) error {
	return c.binder.BindHeaders(c.ctx, dst)
}

// Responder (httpx.Responder)

func (c *echoContext) Status(code int) {
	c.ctx.Response().WriteHeader(code)
}

func (c *echoContext) JSON(code int, v any) error {
	return c.ctx.JSON(code, v)
}

func (c *echoContext) Text(code int, s string) error {
	return c.ctx.String(code, s)
}

func (c *echoContext) NoContent(code int) error {
	return c.ctx.NoContent(code)
}

func (c *echoContext) Bytes(code int, b []byte, contentType string) error {
	if contentType == "" {
		contentType = http.DetectContentType(b)
	}
	return c.ctx.Blob(code, contentType, b)
}

func (c *echoContext) DataFromReader(code int, contentType string, r io.Reader, size int) error {
	if contentType == "" {
		contentType = http.DetectContentType(nil)
	}
	if size >= 0 {
		c.ctx.Response().Header().Set(echo.HeaderContentLength, strconv.Itoa(size))
	}
	return c.ctx.Stream(code, contentType, r)
}

func (c *echoContext) File(path string) error {
	return c.ctx.File(path)
}

func (c *echoContext) Redirect(code int, location string) error {
	return c.ctx.Redirect(code, location)
}

func (c *echoContext) SetHeader(key, value string) {
	c.ctx.Response().Header().Set(key, value)
}

func (c *echoContext) SetCookie(cookie *http.Cookie) {
	if cookie != nil {
		c.ctx.SetCookie(cookie)
	}
}

// StateStore (httpx.StateStore)

func (c *echoContext) Set(key string, val any) {
	c.ctx.Set(key, val)
}

func (c *echoContext) Get(key string) (any, bool) {
	val := c.ctx.Get(key)
	if val == nil {
		return nil, false
	}
	return val, true
}

// Context (context.Context + Next)

func (c *echoContext) Deadline() (deadline time.Time, ok bool) {
	return c.ctx.Request().Context().Deadline()
}

func (c *echoContext) Done() <-chan struct{} {
	return c.ctx.Request().Context().Done()
}

func (c *echoContext) Err() error {
	return c.ctx.Request().Context().Err()
}

func (c *echoContext) Value(key any) any {
	if str, ok := key.(string); ok {
		if val, exists := c.Get(str); exists {
			return val
		}
	}
	return c.ctx.Request().Context().Value(key)
}

func (c *echoContext) Next() error {
	if c.next == nil {
		return nil
	}
	next := c.next
	c.next = nil
	return next(c.ctx)
}

func (c *echoContext) StatusCode() int {
	return c.ctx.Response().Status
}

func (c *echoContext) NativeContext() any {
	return c.ctx
}
