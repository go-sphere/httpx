package fiberx

import (
	"io"
	"mime/multipart"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/go-sphere/httpx"
	"github.com/gofiber/fiber/v3"
)

var _ httpx.Context = (*fiberContext)(nil)

type fiberContext struct {
	ctx          fiber.Ctx
	errorHandler httpx.ErrorHandler
	aborted      atomic.Bool
}

func newFiberContext(ctx fiber.Ctx, errorHandler httpx.ErrorHandler) *fiberContext {
	return &fiberContext{
		ctx:          ctx,
		errorHandler: errorHandler,
	}
}

// Request exposes a common, read-only view over incoming HTTP requests.

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

func (c *fiberContext) FormValue(key string) string {
	return c.ctx.FormValue(key)
}

func (c *fiberContext) FormValues() map[string][]string {
	form := c.ctx.Request().PostArgs()
	if form.Len() == 0 {
		return nil
	}
	out := make(map[string][]string, form.Len())
	for keyBytes, valueBytes := range form.All() {
		key := string(keyBytes)
		out[key] = append(out[key], string(valueBytes))
	}
	return out
}

func (c *fiberContext) FormFile(name string) (*multipart.FileHeader, error) {
	return c.ctx.FormFile(name)
}

func (c *fiberContext) GetBodyRaw() ([]byte, error) {
	return c.ctx.BodyRaw(), nil
}

func (c *fiberContext) Header(key string) string {
	return c.ctx.Get(key)
}

func (c *fiberContext) Cookie(name string) (string, error) {
	value := c.ctx.Request().Header.Cookie(name)
	if value == nil {
		return "", http.ErrNoCookie
	}
	return string(value), nil
}

// Binder standardizes payload decoding across frameworks.

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

// Responder writes responses across frameworks.

func (c *fiberContext) Status(code int) {
	c.ctx.Status(code)
}

func (c *fiberContext) JSON(code int, v any) {
	err := c.ctx.Status(code).JSON(v)
	if err != nil && c.errorHandler != nil {
		c.errorHandler(c, err)
	}
}

func (c *fiberContext) Text(code int, s string) {
	err := c.ctx.Status(code).SendString(s)
	if err != nil && c.errorHandler != nil {
		c.errorHandler(c, err)
	}
}

func (c *fiberContext) Bytes(code int, b []byte, contentType string) {
	if contentType != "" {
		c.ctx.Set(fiber.HeaderContentType, contentType)
	}
	err := c.ctx.Status(code).Send(b)
	if err != nil && c.errorHandler != nil {
		c.errorHandler(c, err)
	}
}

func (c *fiberContext) DataFromReader(code int, contentType string, r io.Reader, size int) {
	if contentType != "" {
		c.ctx.Set(fiber.HeaderContentType, contentType)
	}
	err := c.ctx.Status(code).SendStream(r, size)
	if err != nil && c.errorHandler != nil {
		c.errorHandler(c, err)
	}
}

func (c *fiberContext) File(path string) {
	err := c.ctx.SendFile(path)
	if err != nil && c.errorHandler != nil {
		c.errorHandler(c, err)
	}
}

func (c *fiberContext) Redirect(code int, location string) {
	redirect := c.ctx.Redirect()
	if code > 0 {
		redirect.Status(code)
	}
	err := redirect.To(location)
	if err != nil && c.errorHandler != nil {
		c.errorHandler(c, err)
	}
}

func (c *fiberContext) SetHeader(key, value string) {
	c.ctx.Set(key, value)
}

func (c *fiberContext) SetCookie(cookie *http.Cookie) {
	if cookie == nil {
		return
	}
	fc := &fiber.Cookie{
		Name:     cookie.Name,
		Value:    cookie.Value,
		Path:     cookie.Path,
		Domain:   cookie.Domain,
		Expires:  cookie.Expires,
		MaxAge:   cookie.MaxAge,
		Secure:   cookie.Secure,
		HTTPOnly: cookie.HttpOnly,
		SameSite: mapSameSite(cookie.SameSite),
	}
	if fc.Path == "" {
		fc.Path = "/"
	}
	c.ctx.Cookie(fc)
}

// StateStore carries request-scoped values.

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

// Context standardizes context operations across frameworks.

func (c *fiberContext) Deadline() (deadline time.Time, ok bool) {
	return c.ctx.Deadline()
}

func (c *fiberContext) Done() <-chan struct{} {
	return c.ctx.Done()
}

func (c *fiberContext) Err() error {
	return c.ctx.Err()
}

func (c *fiberContext) Value(key any) any {
	return c.ctx.Value(key)
}

// Aborter allows a handler to short-circuit the remaining chain.

func (c *fiberContext) Abort() {
	if c.aborted.Swap(true) {
		return
	}
	c.ctx.Response().ResetBody()
}

func (c *fiberContext) AbortWithStatus(code int) {
	if code > 0 {
		c.ctx.Status(code)
	}
	c.Abort()
}

func (c *fiberContext) AbortWithStatusError(code int, err error) {
	if err != nil && c.errorHandler != nil {
		c.errorHandler(c, err)
	}
	c.AbortWithStatus(code)
}

func (c *fiberContext) AbortWithStatusJSON(code int, obj interface{}) {
	err := c.ctx.Status(code).JSON(obj)
	if err != nil && c.errorHandler != nil {
		c.errorHandler(c, err)
	}
}

func (c *fiberContext) IsAborted() bool {
	return c.aborted.Load()
}

func mapSameSite(mode http.SameSite) string {
	switch mode {
	case http.SameSiteStrictMode:
		return fiber.CookieSameSiteStrictMode
	case http.SameSiteNoneMode:
		return fiber.CookieSameSiteNoneMode
	case http.SameSiteDefaultMode:
		return fiber.CookieSameSiteDisabled
	default:
		return fiber.CookieSameSiteLaxMode
	}
}
