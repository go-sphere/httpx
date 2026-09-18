package echox

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
	"net/url"
	"strconv"
	"strings"

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

// FromEcho wraps an echo.Context as httpx.Context. Use it from an echo
// HTTPErrorHandler or native middleware to write through httpx helpers.
//
// The returned context carries no downstream handler, so Next is a no-op that
// returns nil: this is a writing and request-inspection handle, not a place to
// resume a chain from.
func FromEcho(ctx echo.Context) httpx.Context {
	return newEchoContext(ctx)
}

// Request (httpx.Request)

func (c *echoContext) Method() string {
	return c.ctx.Request().Method
}

func (c *echoContext) Path() string {
	return c.ctx.Request().URL.Path
}

// FullPath reports the route pattern as it was registered. When the pattern
// carried a named wildcard, FixWildcardPathIfNeed rewrote it to echo's
// anonymous form ("/files/*filepath" -> "/files/*") because echo has no named
// wildcards — that rewrite is this adapter's business and must not surface
// here, or a caller matching on FullPath (downstream auth and rate limiting do)
// would see a different pattern than it registered. The trailing-byte check
// keeps a route without a wildcard from paying the map lookup.
func (c *echoContext) FullPath() string {
	pattern := c.ctx.Path()
	if lastCharIs('*', pattern) {
		if route, ok := lookupWildcardRoute(pattern); ok {
			return route.pattern
		}
	}
	return pattern
}

func (c *echoContext) ClientIP() string {
	return c.ctx.RealIP()
}

func (c *echoContext) Param(key string) string {
	v := c.ctx.Param(key)
	if v == "" && key != "*" {
		// Named wildcard rewritten to "*" at registration time.
		if c.wildcardParamName() == key {
			v = c.ctx.Param("*")
		}
	}
	return c.paramValue(v)
}

func (c *echoContext) Params() map[string]string {
	names := c.ctx.ParamNames()
	if len(names) == 0 {
		return nil
	}
	values := c.ctx.ParamValues()
	decode := c.decodesParams()
	out := make(map[string]string, len(names))
	for i, name := range names {
		value := ""
		if i < len(values) {
			value = values[i]
		}
		if decode {
			out[name] = decodeParamValue(value)
		} else {
			out[name] = value
		}
	}
	if v, exists := out["*"]; exists {
		if name := c.wildcardParamName(); name != "" {
			out[name] = v
			// "*" is this adapter's normalization artifact: the route was
			// registered as /*name, so Params must read the same as on an
			// adapter with native named wildcards. An anonymous /* route
			// has no entry here and keeps its "*" key.
			delete(out, "*")
		}
	}
	return out
}

// wildcardParamName reports the name the matched route's wildcard was
// registered with, or "" when the route has no named wildcard. echo only knows
// the parameter as "*"; every reading of the parameter set (Param, Params,
// BindURI) resolves it back through here so none of them can disagree.
func (c *echoContext) wildcardParamName() string {
	route, ok := lookupWildcardRoute(c.ctx.Path())
	if !ok {
		return ""
	}
	return route.param
}

// paramValue makes a route parameter read the same as on gin, hertz and stdx.
//
// echo matches on URL.RawPath when the standard library set it, which happens
// exactly when a segment carries an escape whose decoded form re-encodes
// differently — "%2F" above all, since "/" is not escaped on the way back. The
// parameter then arrives percent-encoded where the other adapters hand back the
// decoded value. An ordinary request leaves RawPath empty and echo already
// matched the decoded path, so the checks are ordered to cost a pointer
// comparison and, only past that, a byte scan.
//
// Gating on RawPath rather than on the '%' alone is what keeps this from
// decoding twice: "/decode/a%252Fb" arrives with RawPath empty and echo's
// parameter already reads "a%2Fb", which is the value the caller sent.
func (c *echoContext) paramValue(v string) string {
	if !c.decodesParams() {
		return v
	}
	return decodeParamValue(v)
}

// decodesParams reports whether route parameters of *this* request still carry
// percent-escapes the other adapters would have decoded.
func (c *echoContext) decodesParams() bool {
	return c.ctx.Request().URL.RawPath != ""
}

func decodeParamValue(v string) string {
	if !strings.ContainsRune(v, '%') {
		return v
	}
	decoded, err := url.PathUnescape(v)
	if err != nil {
		// A malformed escape is not an escape; hand back what was matched.
		return v
	}
	return decoded
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
	return httpx.WrapBindError(json.NewDecoder(c.ctx.Request().Body).Decode(dst))
}

func (c *echoContext) BindQuery(dst any) error {
	return httpx.WrapBindError(c.binder.BindQueryParams(c.ctx, dst))
}

func (c *echoContext) BindForm(dst any) error {
	return httpx.WrapBindError(bindForm(dst, c.ctx))
}

func (c *echoContext) BindURI(dst any) error {
	return httpx.WrapBindError(c.bindURIWithForm(dst))
}

func (c *echoContext) BindHeader(dst any) error {
	return httpx.WrapBindError(c.binder.BindHeaders(c.ctx, dst))
}

// Responder (httpx.Responder)

func (c *echoContext) Status(code int) {
	if !c.ctx.Response().Committed {
		c.ctx.Response().Status = code
	}
}

func (c *echoContext) JSON(code int, v any) error {
	// echo labels JSON "application/json" where the other four adapters add
	// "; charset=utf-8". echo only fills the header when it is still empty, so
	// writing it first aligns the label while leaving a Content-Type the
	// handler chose itself (a "+json" media type, say) alone.
	if header := c.ctx.Response().Header(); header.Get(echo.HeaderContentType) == "" {
		header.Set(echo.HeaderContentType, "application/json; charset=utf-8")
	}
	return c.ctx.JSON(code, v)
}

func (c *echoContext) Text(code int, s string) error {
	// Pre-set the header with the lowercase charset used by the other
	// adapters (echo's default is "charset=UTF-8"); echo only sets the
	// content type when it is still empty.
	c.ctx.Response().Header().Set(echo.HeaderContentType, "text/plain; charset=utf-8")
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

func (c *echoContext) DataFromReader(code int, contentType string, r io.Reader, size int64) error {
	if rc, ok := r.(io.Closer); ok {
		defer func() {
			_ = rc.Close()
		}()
	}
	if contentType == "" {
		contentType = http.DetectContentType(nil)
	}
	if size >= 0 {
		c.ctx.Response().Header().Set(echo.HeaderContentLength, strconv.FormatInt(size, 10))
	}
	return c.ctx.Stream(code, contentType, r)
}

func (c *echoContext) File(path string) error {
	return c.ctx.File(path)
}

func (c *echoContext) Redirect(code int, location string) error {
	if !httpx.ValidRedirectCode(code) {
		return httpx.NewInternalServerError(fmt.Sprintf("cannot redirect with status code %d", code))
	}
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

// Context (context.Context accessor + Next)

func (c *echoContext) Context() context.Context {
	return c.ctx.Request().Context()
}

func (c *echoContext) SetContext(ctx context.Context) {
	c.ctx.SetRequest(c.ctx.Request().WithContext(ctx))
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

// Flush implements httpx.Flusher: the first flush commits status and headers.
func (c *echoContext) Flush() error {
	resp := c.ctx.Response()
	if !resp.Committed {
		resp.WriteHeader(resp.Status)
	}
	// Go under echo.Response.Flush(), which panics when unsupported; the
	// controller returns an error instead.
	return http.NewResponseController(resp.Writer).Flush()
}

// Stream implements httpx.Streamer: each write inside fn is flushed to the
// client immediately.
func (c *echoContext) Stream(code int, contentType string, fn func(w io.Writer) error) error {
	resp := c.ctx.Response()
	if contentType != "" {
		resp.Header().Set(echo.HeaderContentType, contentType)
	}
	resp.WriteHeader(code)
	return fn(echoFlushWriter{resp: resp, rc: http.NewResponseController(resp.Writer)})
}

type echoFlushWriter struct {
	resp *echo.Response
	rc   *http.ResponseController
}

func (fw echoFlushWriter) Write(p []byte) (int, error) {
	n, err := fw.resp.Write(p)
	if err != nil {
		return n, err
	}
	if err := fw.rc.Flush(); err != nil && !errors.Is(err, http.ErrNotSupported) {
		return n, err
	}
	return n, nil
}
