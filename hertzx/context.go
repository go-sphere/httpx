package hertzx

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strings"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server/render"
	"github.com/cloudwego/hertz/pkg/protocol/http1/resp"
	"github.com/go-sphere/httpx"
)

var _ httpx.Context = (*hertzContext)(nil)

type hertzContext struct {
	ctx     *app.RequestContext
	baseCtx context.Context
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
	body, err := c.ctx.Request.BodyE()
	if err != nil {
		return nil, err
	}
	// Copy out of hertz's pooled request buffer so the bytes stay valid after
	// the request completes, which is what the BodyAccess contract promises.
	// Handlers that only read the body during the request can avoid the copy
	// with BodyReader or the native context.
	return bytes.Clone(body), nil
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
	return httpx.WrapBindError(c.ctx.BindJSON(dst))
}

func (c *hertzContext) BindQuery(dst any) error {
	return httpx.WrapBindError(c.ctx.BindQuery(dst))
}

func (c *hertzContext) BindForm(dst any) error {
	return httpx.WrapBindError(c.ctx.BindForm(dst))
}

func (c *hertzContext) BindURI(dst any) error {
	return httpx.WrapBindError(c.bindURIWithForm(dst))
}

func (c *hertzContext) BindHeader(dst any) error {
	return httpx.WrapBindError(c.ctx.BindHeader(dst))
}

// Responder (httpx.Responder)

func (c *hertzContext) Status(code int) {
	if !hertzResponseCommitted(c.ctx) {
		c.ctx.Status(code)
	}
}

func (c *hertzContext) JSON(code int, v any) error {
	// Use Hertz's configured marshaler (including ResetJSONMarshal), while
	// returning encoding failures instead of letting RequestContext.JSON panic.
	r := render.JSONRender{Data: v}
	if code >= 100 && code < 200 || code == http.StatusNoContent || code == http.StatusNotModified {
		c.ctx.Status(code)
		r.WriteContentType(&c.ctx.Response)
		c.ctx.Set(responseCommittedKey, true)
		return nil
	}
	if err := r.Render(&c.ctx.Response); err != nil {
		return err
	}
	c.ctx.Status(code)
	return nil
}

func (c *hertzContext) Text(code int, s string) error {
	c.ctx.String(code, s)
	if s == "" {
		c.ctx.Set(responseCommittedKey, true)
	}
	return nil
}

func (c *hertzContext) NoContent(code int) error {
	c.ctx.Status(code)
	c.ctx.Response.ResetBody()
	c.ctx.Set(responseCommittedKey, true)
	return nil
}

func (c *hertzContext) Bytes(code int, b []byte, contentType string) error {
	if contentType == "" {
		contentType = http.DetectContentType(b)
	}
	c.ctx.Data(code, contentType, b)
	if len(b) == 0 {
		c.ctx.Set(responseCommittedKey, true)
	}
	return nil
}

func (c *hertzContext) DataFromReader(code int, contentType string, r io.Reader, size int64) error {
	if contentType != "" {
		c.ctx.SetContentType(contentType)
	}
	c.ctx.Status(code)
	c.ctx.SetBodyStream(r, streamSize(size))
	return nil
}

// streamSize narrows the contract's int64 size to the int that hertz's
// SetBodyStream takes. A length that does not fit in int (only reachable on a
// 32-bit build) degrades to -1 — unknown size, chunked transfer — because the
// alternative, a truncating conversion, would advertise a Content-Length that
// does not match the body and corrupt the response. The body itself is still
// streamed in full either way.
func streamSize(size int64) int {
	if size < 0 || int64(int(size)) != size {
		return -1
	}
	return int(size)
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
	c.ctx.Set(responseCommittedKey, true)
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

// Context (context.Context accessor)

func (c *hertzContext) Context() context.Context {
	return c.baseCtx
}

func (c *hertzContext) SetContext(ctx context.Context) {
	c.baseCtx = ctx
}

func (c *hertzContext) StatusCode() int {
	return c.ctx.Response.StatusCode()
}

func (c *hertzContext) NativeContext() any {
	return c.ctx
}

// Flush implements httpx.Flusher. Hertz buffers the response until the
// handler returns, so the first flush hijacks the response with a chunked
// body writer (transferring anything already buffered) and commits status
// and headers. Without a live connection (in-process test dispatch) it is a
// no-op and the response stays buffered.
func (c *hertzContext) Flush() error {
	if c.ctx.Response.GetHijackWriter() == nil {
		w := c.ctx.GetWriter()
		if w == nil {
			return nil
		}
		body := bytes.Clone(c.ctx.Response.Body())
		c.ctx.Response.ResetBody()
		c.ctx.Response.HijackWriter(resp.NewChunkedBodyWriter(&c.ctx.Response, w))
		if len(body) > 0 {
			if _, err := c.ctx.Response.GetHijackWriter().Write(body); err != nil {
				return err
			}
		}
	}
	return c.ctx.Flush()
}

// Stream implements httpx.Streamer: each write inside fn is flushed to the
// client immediately.
func (c *hertzContext) Stream(code int, contentType string, fn func(w io.Writer) error) error {
	if contentType != "" {
		c.ctx.SetContentType(contentType)
	}
	c.ctx.Status(code)
	// The response is committed from here on: the status and content type are
	// decided and, over a real connection, already flushed. Recording it keeps
	// an error returned by fn from being rendered over the stream.
	c.ctx.Set(responseCommittedKey, true)
	if err := c.Flush(); err != nil {
		return err
	}
	return fn(hertzFlushWriter{c: c})
}

type hertzFlushWriter struct {
	c *hertzContext
}

func (fw hertzFlushWriter) Write(p []byte) (int, error) {
	// AppendBody routes through the hijack writer when one is set; in
	// buffered (test) mode it accumulates in the response body.
	fw.c.ctx.Response.AppendBody(p)
	if err := fw.c.Flush(); err != nil {
		return 0, err
	}
	return len(p), nil
}
