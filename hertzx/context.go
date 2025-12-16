package hertzx

import (
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol"
	"github.com/go-sphere/httpx"
)

var _ httpx.Context = (*hertzContext)(nil)

type hertzContext struct {
	ctx          *app.RequestContext
	baseCtx      context.Context
	errorHandler httpx.ErrorHandler
}

func newHertzContext(ctx context.Context, rc *app.RequestContext, eh httpx.ErrorHandler) *hertzContext {
	return &hertzContext{
		ctx:          rc,
		baseCtx:      ctx,
		errorHandler: eh,
	}
}

// Request exposes a common, read-only view over incoming HTTP requests.

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

func (c *hertzContext) FormValue(key string) string {
	return string(c.ctx.FormValue(key))
}

func (c *hertzContext) FormValues() map[string][]string {
	form := c.ctx.PostArgs()
	out := make(map[string][]string)
	if form.Len() > 0 {
		form.VisitAll(func(k, v []byte) {
			key := string(k)
			out[key] = append(out[key], string(v))
		})
	}
	if mf, err := c.ctx.MultipartForm(); err == nil && mf.Value != nil {
		for k, values := range mf.Value {
			out[k] = append(out[k], values...)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func (c *hertzContext) FormFile(name string) (*multipart.FileHeader, error) {
	return c.ctx.FormFile(name)
}

func (c *hertzContext) GetBodyRaw() ([]byte, error) {
	return c.ctx.Request.BodyE()
}

func (c *hertzContext) Header(key string) string {
	return string(c.ctx.GetHeader(key))
}

func (c *hertzContext) Cookie(name string) (string, error) {
	val := c.ctx.Cookie(name)
	if val == nil {
		return "", http.ErrNoCookie
	}
	return string(val), nil
}

// Binder standardizes payload decoding across frameworks.

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
	return c.ctx.BindPath(dst)
}

func (c *hertzContext) BindHeader(dst any) error {
	return c.ctx.BindHeader(dst)
}

// Responder writes responses across frameworks.

func (c *hertzContext) Status(code int) {
	c.ctx.Status(code)
}

func (c *hertzContext) JSON(code int, v any) {
	c.ctx.JSON(code, v)
}

func (c *hertzContext) Text(code int, s string) {
	c.ctx.String(code, s)
}

func (c *hertzContext) Bytes(code int, b []byte, contentType string) {
	c.ctx.Data(code, contentType, b)
}

func (c *hertzContext) DataFromReader(code int, contentType string, r io.Reader, size int) {
	if contentType != "" {
		c.ctx.SetContentType(contentType)
	}
	c.ctx.Status(code)
	c.ctx.SetBodyStream(r, size)
}

func (c *hertzContext) File(path string) {
	c.ctx.File(path)
}

func (c *hertzContext) Redirect(code int, location string) {
	c.ctx.Redirect(code, []byte(location))
}

func (c *hertzContext) SetHeader(key, value string) {
	c.ctx.Header(key, value)
}

func (c *hertzContext) SetCookie(cookie *http.Cookie) {
	if cookie == nil {
		return
	}
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

// StateStore carries request-scoped values.

func (c *hertzContext) Set(key string, val any) {
	c.ctx.Set(key, val)
}

func (c *hertzContext) Get(key string) (any, bool) {
	return c.ctx.Get(key)
}

// Context standardizes context operations across frameworks.

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

// Aborter allows a handler to short-circuit the remaining chain.

func (c *hertzContext) Abort() {
	c.ctx.Abort()
}

func (c *hertzContext) AbortWithStatus(code int) {
	c.ctx.AbortWithStatus(code)
}

func (c *hertzContext) AbortWithStatusError(code int, err error) {
	if err != nil && c.errorHandler != nil {
		c.errorHandler(c, err)
	}
	c.ctx.AbortWithStatus(code)
}

func (c *hertzContext) AbortWithStatusJSON(code int, obj interface{}) {
	c.ctx.AbortWithStatusJSON(code, obj)
}

func (c *hertzContext) IsAborted() bool {
	return c.ctx.IsAborted()
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
