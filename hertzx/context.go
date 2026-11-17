package hertzx

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server/render"
	"github.com/cloudwego/hertz/pkg/protocol"
	"github.com/cloudwego/hertz/pkg/protocol/http1/resp"
	"github.com/go-sphere/httpx"
)

var (
	_ httpx.Context              = (*hertzContext)(nil)
	_ httpx.ResponseHeaderEditor = (*hertzContext)(nil)
)

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
	// net/http moves Host out of the header map, so the other adapters report
	// it as unset; hertz's GetHeader special-cases it and would answer.
	if strings.EqualFold(key, "Host") {
		return ""
	}
	return string(c.ctx.GetHeader(key))
}

func (c *hertzContext) Headers() map[string][]string {
	var out map[string][]string
	c.ctx.Request.Header.VisitAll(func(k, v []byte) {
		key := textproto.CanonicalMIMEHeaderKey(string(k))
		if key == "Host" {
			// net/http moves Host out of the header map, so gin/echo never
			// expose it here; filter it for cross-adapter consistency.
			return
		}
		if out == nil {
			out = make(map[string][]string)
		}
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
	var out map[string]string
	c.ctx.Request.Header.VisitAllCookie(func(k, v []byte) {
		if out == nil {
			out = make(map[string]string)
		}
		out[string(k)] = string(v)
	})
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
	// BodyStream never returns nil (an absent stream is protocol.NoBody), and
	// a server not in streaming mode buffers the body instead, so the stream
	// is only the body when IsBodyStream says so.
	if c.ctx.Request.IsBodyStream() {
		return httpx.NewReadCloser(c.ctx.Request.BodyStream(), c.ctx.Request.CloseBodyStream)
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
	return httpx.WrapBindError(c.bindFormBody(dst))
}

func (c *hertzContext) BindURI(dst any) error {
	return httpx.WrapBindError(c.bindURIWithForm(dst))
}

func (c *hertzContext) BindHeader(dst any) error {
	header := &c.ctx.Request.Header
	host := header.Host()
	if len(host) == 0 {
		return httpx.WrapBindError(c.ctx.BindHeader(dst))
	}
	saved := bytes.Clone(host)
	header.SetHostBytes(nil)
	defer header.SetHostBytes(saved)
	return httpx.WrapBindError(c.ctx.BindHeader(dst))
}

// Responder (httpx.Responder)

// writeBody runs one response-writing call under the post-commit rules
// httpx.Responder documents, and is the single place this adapter applies them.
//
// gin, echo and stdx inherit those rules from the http.ResponseWriter they
// write through: once the header is out, WriteHeader is ignored, Header()
// changes are dropped and Write appends. hertz offers no writer to intercept —
// status, headers and body sit in a protocol.Response that is serialized only
// after the handler returns — so all three stay freely rewritable past the
// point at which a client has conceptually received them. The buffer is the
// only seam there is, which is why the rules live here rather than in each
// Responder method.
//
// Before the first write there is nothing to protect and the call runs
// untouched. Afterwards the header block is snapshotted and the body emptied,
// so that what the call produces can be told apart from what was already
// there; the snapshot then goes back over whatever the call set, leaving only
// its bytes, appended. Body materializes a body stream the call may have
// installed, so DataFromReader appends like any other write, and hertz
// recomputes Content-Length from the body as it serializes, so the restored
// one cannot go stale.
func (c *hertzContext) writeBody(write func() error) error {
	if !c.Committed() {
		err := write()
		if err == nil {
			markCommitted(c.ctx)
		}
		return err
	}
	resp := &c.ctx.Response
	if resp.GetHijackWriter() != nil || resp.IsBodyStream() {
		// The bytes are already going out over the connection (Stream/SSE) or
		// have been handed to hertz as a stream: there is no buffer left to
		// append to, and nothing still mutable to freeze.
		return nil
	}
	var header protocol.ResponseHeader
	resp.Header.CopyTo(&header)
	sent := bytes.Clone(resp.Body())
	resp.ResetBody()
	err := write()
	body := append(sent, resp.Body()...)
	header.CopyTo(&resp.Header)
	resp.SetBody(body)
	return err
}

func (c *hertzContext) Status(code int) {
	if !c.Committed() {
		c.ctx.Status(code)
	}
}

func (c *hertzContext) commitsBodyless(code int) bool {
	if c.Committed() {
		return bodylessStatus(c.ctx.Response.StatusCode())
	}
	return bodylessStatus(code)
}

func (c *hertzContext) JSON(code int, v any) error {
	bl := c.commitsBodyless(code)
	if bl {
		var probe protocol.Response
		if err := (render.JSONRender{Data: v}).Render(&probe); err != nil {
			return err
		}
	}
	return c.writeBody(func() error {
		// Use Hertz's configured marshaler (including ResetJSONMarshal), while
		// returning encoding failures instead of letting RequestContext.JSON
		// panic. The render always sets its own Content-Type, so a preset one
		// (or none, after an encode error) is put back.
		header := &c.ctx.Response.Header
		preset := bytes.Clone(explicitContentType(header))
		r := render.JSONRender{Data: v}
		if bl {
			c.ctx.Status(code)
			c.ctx.Response.ResetBody()
			if len(preset) == 0 {
				r.WriteContentType(&c.ctx.Response)
			}
			return nil
		}
		if err := r.Render(&c.ctx.Response); err != nil {
			header.SetContentTypeBytes(preset)
			return err
		}
		if len(preset) > 0 {
			header.SetContentTypeBytes(preset)
		}
		c.ctx.Status(code)
		return nil
	})
}

func (c *hertzContext) Text(code int, s string) error {
	bl := c.commitsBodyless(code)
	return c.writeBody(func() error {
		c.ctx.SetContentType("text/plain; charset=utf-8")
		c.ctx.Status(code)
		if bl {
			c.ctx.Response.ResetBody()
			return nil
		}
		_, err := c.ctx.WriteString(s)
		return err
	})
}

func (c *hertzContext) NoContent(code int) error {
	return c.writeBody(func() error {
		c.ctx.Status(code)
		c.ctx.Response.ResetBody()
		return nil
	})
}

func (c *hertzContext) Bytes(code int, b []byte, contentType string) error {
	if contentType == "" {
		contentType = http.DetectContentType(b)
	}
	bl := c.commitsBodyless(code)
	return c.writeBody(func() error {
		c.ctx.SetContentType(contentType)
		c.ctx.Status(code)
		if bl {
			c.ctx.Response.ResetBody()
			return nil
		}
		_, err := c.ctx.Write(b)
		return err
	})
}

func (c *hertzContext) DataFromReader(code int, contentType string, r io.Reader, size int64) error {
	bl := c.commitsBodyless(code)
	if bl {
		if closer, ok := r.(io.Closer); ok {
			defer func() { _ = closer.Close() }()
		}
		return c.writeBody(func() error {
			if contentType != "" {
				c.ctx.SetContentType(contentType)
			}
			c.ctx.Status(code)
			c.ctx.Response.ResetBody()
			return nil
		})
	}
	return c.writeBody(func() error {
		if contentType != "" {
			c.ctx.SetContentType(contentType)
		}
		c.ctx.Status(code)
		if r == nil {
			c.ctx.Response.ResetBody()
			return nil
		}
		c.ctx.SetBodyStream(r, streamSize(size))
		return nil
	})
}

// bodylessStatus reports whether code is a status whose response has no body
// (1xx, 204, 304).
func bodylessStatus(code int) bool {
	return code >= 100 && code < 200 || code == http.StatusNoContent || code == http.StatusNotModified
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
	if err := httpx.CheckServeFile(path); err != nil {
		return err
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return httpx.InternalServerError(err)
	}
	return c.writeBody(func() error {
		// hertz serves a file by rewriting the request URI to its path, which
		// would leave Path and Query reporting the file for every layer that
		// reads them after the handler. The path is escaped so a name holding
		// '#', '?' or '%' survives being parsed as a URI.
		uri := string(c.ctx.Request.Header.RequestURI())
		c.ctx.File(escapeFilePath(abs))
		c.ctx.Request.SetRequestURI(uri)
		return nil
	})
}

// escapeFilePath percent-escapes each segment of an absolute file path for use
// as a request URI.
func escapeFilePath(path string) string {
	segments := strings.Split(filepath.ToSlash(path), "/")
	for i, s := range segments {
		segments[i] = url.PathEscape(s)
	}
	return strings.Join(segments, "/")
}

func (c *hertzContext) Redirect(code int, location string) error {
	if !httpx.ValidRedirectCode(code) {
		return httpx.NewInternalServerError(fmt.Sprintf("cannot redirect with status code %d", code))
	}
	return c.writeBody(func() error {
		// hertz's Redirect does the same but rewrites 300 to 302.
		c.ctx.Response.Header.Set("Location", location)
		c.ctx.Status(code)
		return nil
	})
}

func (c *hertzContext) SetHeader(key, value string) {
	if c.Committed() {
		return
	}
	if strings.EqualFold(key, "Set-Cookie") {
		c.ctx.Response.Header.DelAllCookies()
	}
	// Not hertz's c.Header, which deletes the header for an empty value.
	c.ctx.Response.Header.Set(key, value)
}

func (c *hertzContext) AddHeader(key, value string) {
	if c.Committed() {
		return
	}
	c.ctx.Response.Header.Add(key, value)
}

func (c *hertzContext) ResponseHeaderValues(key string) []string {
	if strings.EqualFold(key, "Content-Type") {
		// GetAll would report hertz's default for a response nothing labelled.
		if ct := explicitContentType(&c.ctx.Response.Header); len(ct) > 0 {
			return []string{string(ct)}
		}
		return nil
	}
	if strings.EqualFold(key, "Set-Cookie") {
		var cookies []string
		c.ctx.Response.Header.VisitAllCookie(func(_, v []byte) {
			cookies = append(cookies, string(v))
		})
		return cookies
	}
	values := c.ctx.Response.Header.GetAll(key)
	// hertz answers Set-Cookie with one empty string when no cookie is set.
	if len(values) == 0 || len(values) == 1 && values[0] == "" {
		return nil
	}
	return values
}

func (c *hertzContext) SetCookie(cookie *http.Cookie) {
	if cookie == nil || c.Committed() {
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
	if ctx != nil {
		c.baseCtx = ctx
	}
}

func (c *hertzContext) StatusCode() int {
	return c.ctx.Response.StatusCode()
}

// Committed reports whether the response header has been written. hertz
// buffers the whole response, so there is no framework flag to read: the
// predicate is hertzResponseCommitted, the same one the router consults before
// letting the error handler render over a response.
func (c *hertzContext) Committed() bool {
	return hertzResponseCommitted(c.ctx)
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
	return flushResponse(c.ctx)
}

// flushResponse is Flush on rc, shared with the net/http writer HandleStd and
// AdaptStdMiddleware hand out.
func flushResponse(rc *app.RequestContext) error {
	if rc.Response.GetHijackWriter() == nil {
		w := rc.GetWriter()
		if w == nil {
			return nil
		}
		commitPendingStdWriters(rc)
		body := bytes.Clone(rc.Response.Body())
		rc.Response.ResetBody()
		rc.Response.HijackWriter(resp.NewChunkedBodyWriter(&rc.Response, w))
		if len(body) > 0 {
			if _, err := rc.Response.GetHijackWriter().Write(body); err != nil {
				return err
			}
		}
	}
	// hertz's chunked writer sends the header only with the first chunk; a
	// flush must send it even when no body has been written yet.
	if hw, ok := rc.Response.GetHijackWriter().(interface{ WriteHeader() error }); ok {
		if err := hw.WriteHeader(); err != nil {
			return err
		}
	}
	return rc.Flush()
}

// Stream implements httpx.Streamer: each write inside fn is flushed to the
// client immediately. On a committed response code and contentType are
// ignored and fn's bytes follow the body already written.
func (c *hertzContext) Stream(code int, contentType string, fn func(w io.Writer) error) error {
	if !c.Committed() {
		if contentType != "" {
			c.ctx.SetContentType(contentType)
		}
		c.ctx.Status(code)
	}
	err := c.Flush()
	// Record committed state after Flush: in in-process tests no hijack writer
	// is attached, so without markCommitted a following error or write could see
	// the response as still uncommitted.
	markCommitted(c.ctx)
	if err != nil {
		return err
	}
	if fn == nil {
		return nil
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
