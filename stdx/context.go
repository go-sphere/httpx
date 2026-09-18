package stdx

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/textproto"
	"net/url"

	"github.com/go-sphere/httpx"
)

var _ httpx.Context = (*stdContext)(nil)

// defaultMultipartMemory mirrors gin's default: parse up to 32 MiB of a
// multipart form in memory, spill the rest to temporary files.
const defaultMultipartMemory = 32 << 20

// stdContext is the whole per-request state: the response writer wrapper, the
// chain position and the state store live inline, so a request needs exactly
// one allocation for the context itself.
type stdContext struct {
	rw     responseWriter
	req    *http.Request
	engine *Engine
	native Native

	route *route
	// values are the captured parameters, positionally matching route.params.
	// valueBuf backs them so routes with few parameters never allocate.
	values   []string
	valueBuf [8]string

	chain []httpx.Middleware
	leaf  httpx.Handler
	index int

	keys map[string]any
}

func (c *stdContext) reset(e *Engine, w http.ResponseWriter, req *http.Request) {
	c.rw = responseWriter{ResponseWriter: w, status: http.StatusOK}
	c.req = req
	c.engine = e
	c.native = Native{c: c}
	c.route = nil
	c.values = c.valueBuf[:0]
	c.chain = nil
	c.leaf = nil
	c.index = 0
	c.keys = nil
}

// Native is what AsNativeContext yields on this adapter: the plain net/http
// pair this adapter runs on. Replacing the request (or the writer) here is
// visible to the rest of the chain, which is what lets AdaptStdMiddleware
// forward request mutations and writer wrapping.
type Native struct {
	c *stdContext
}

func (n *Native) Request() *http.Request                       { return n.c.req }
func (n *Native) SetRequest(req *http.Request)                 { n.c.req = req }
func (n *Native) ResponseWriter() http.ResponseWriter          { return &n.c.rw }
func (n *Native) SetWriter(w http.ResponseWriter)              { n.c.rw.ResponseWriter = w }
func (n *Native) Engine() *Engine                              { return n.c.engine }
func (n *Native) Written() bool                                { return n.c.rw.written }
func (n *Native) MarkWritten(status int)                       { n.c.rw.markWritten(status) }
func (n *Native) Unwrap() (http.ResponseWriter, *http.Request) { return &n.c.rw, n.c.req }

// Request (httpx.Request)

func (c *stdContext) Method() string { return c.req.Method }

func (c *stdContext) Path() string { return c.req.URL.Path }

func (c *stdContext) FullPath() string {
	if c.route == nil {
		return ""
	}
	return c.route.pattern
}

func (c *stdContext) ClientIP() string {
	return c.engine.clientIP(c.req)
}

func (c *stdContext) Param(key string) string {
	if c.route == nil {
		return ""
	}
	for i, name := range c.route.params {
		if name == key && i < len(c.values) {
			return c.values[i]
		}
	}
	return ""
}

func (c *stdContext) Params() map[string]string {
	if c.route == nil || len(c.route.params) == 0 {
		return nil
	}
	out := make(map[string]string, len(c.route.params))
	for i, name := range c.route.params {
		if i < len(c.values) {
			out[name] = c.values[i]
		}
	}
	return out
}

func (c *stdContext) Query(key string) string {
	return c.req.URL.Query().Get(key)
}

func (c *stdContext) Queries() map[string][]string {
	queries := c.req.URL.Query()
	if len(queries) == 0 {
		return nil
	}
	out := make(map[string][]string, len(queries))
	for k, v := range queries {
		out[k] = append([]string(nil), v...)
	}
	return out
}

func (c *stdContext) RawQuery() string { return c.req.URL.RawQuery }

func (c *stdContext) Header(key string) string { return c.req.Header.Get(key) }

func (c *stdContext) Headers() map[string][]string {
	src := c.req.Header
	if len(src) == 0 {
		return nil
	}
	out := make(map[string][]string, len(src))
	for k, v := range src {
		out[textproto.CanonicalMIMEHeaderKey(k)] = append([]string(nil), v...)
	}
	return out
}

func (c *stdContext) Cookie(name string) (string, error) {
	cookie, err := c.req.Cookie(name)
	if err != nil {
		return "", http.ErrNoCookie
	}
	return cookie.Value, nil
}

func (c *stdContext) Cookies() map[string]string {
	raw := c.req.Cookies()
	if len(raw) == 0 {
		return nil
	}
	out := make(map[string]string, len(raw))
	for _, cookie := range raw {
		out[cookie.Name] = cookie.Value
	}
	return out
}

func (c *stdContext) FormValue(key string) string { return c.req.FormValue(key) }

func (c *stdContext) MultipartForm() (*multipart.Form, error) {
	if c.req.MultipartForm == nil {
		if err := c.req.ParseMultipartForm(defaultMultipartMemory); err != nil {
			return nil, err
		}
	}
	return c.req.MultipartForm, nil
}

func (c *stdContext) FormFile(name string) (*multipart.FileHeader, error) {
	if c.req.MultipartForm == nil {
		if err := c.req.ParseMultipartForm(defaultMultipartMemory); err != nil {
			return nil, err
		}
	}
	file, header, err := c.req.FormFile(name)
	if err != nil {
		return nil, err
	}
	_ = file.Close()
	return header, nil
}

func (c *stdContext) BodyRaw() ([]byte, error) {
	if c.req.Body == nil {
		return nil, nil
	}
	body, err := io.ReadAll(c.req.Body)
	if err != nil {
		return nil, err
	}
	_ = c.req.Body.Close()
	// Restore the body so a binder running after this still sees it. The
	// returned slice is a fresh copy the caller owns (BodyAccess contract).
	c.req.Body = io.NopCloser(bytes.NewReader(body))
	return body, nil
}

func (c *stdContext) BodyReader() io.ReadCloser {
	if c.req.Body != nil {
		return c.req.Body
	}
	return http.NoBody
}

// Binder (httpx.Binder)

func (c *stdContext) BindJSON(dst any) error {
	if c.req.Body == nil {
		return httpx.WrapBindError(io.EOF)
	}
	if err := json.NewDecoder(c.req.Body).Decode(dst); err != nil {
		return httpx.WrapBindError(err)
	}
	return httpx.WrapBindError(validateStruct(dst))
}

func (c *stdContext) BindQuery(dst any) error {
	if err := queryDecoder.Decode(dst, c.req.URL.Query()); err != nil {
		return httpx.WrapBindError(err)
	}
	return httpx.WrapBindError(validateStruct(dst))
}

func (c *stdContext) BindForm(dst any) error {
	if err := c.req.ParseForm(); err != nil {
		return httpx.WrapBindError(err)
	}
	values := c.req.PostForm
	if c.req.MultipartForm == nil && isMultipart(c.req) {
		if err := c.req.ParseMultipartForm(defaultMultipartMemory); err != nil {
			return httpx.WrapBindError(err)
		}
	}
	if c.req.MultipartForm != nil && len(c.req.MultipartForm.Value) > 0 {
		values = url.Values(c.req.MultipartForm.Value)
	}
	if err := formDecoder.Decode(dst, values); err != nil {
		return httpx.WrapBindError(err)
	}
	return httpx.WrapBindError(validateStruct(dst))
}

func (c *stdContext) BindURI(dst any) error {
	if c.route != nil && len(c.route.params) > 0 {
		values := make(url.Values, len(c.route.params))
		for i, name := range c.route.params {
			if i < len(c.values) {
				values.Set(name, c.values[i])
			}
		}
		if err := uriDecoder.Decode(dst, values); err != nil {
			return httpx.WrapBindError(err)
		}
	}
	return httpx.WrapBindError(validateStruct(dst))
}

func (c *stdContext) BindHeader(dst any) error {
	if err := headerDecoder.Decode(dst, url.Values(c.req.Header)); err != nil {
		return httpx.WrapBindError(err)
	}
	return httpx.WrapBindError(validateStruct(dst))
}

// Responder (httpx.Responder)

func (c *stdContext) Status(code int) {
	// Records the status without producing a response: a later Write or the
	// engine's final commit uses it. A bare Status must not count as a
	// committed response, or an error after it would be swallowed.
	c.rw.status = code
}

func (c *stdContext) JSON(code int, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	c.rw.Header().Set("Content-Type", "application/json; charset=utf-8")
	c.rw.WriteHeader(code)
	if bodyless(code) {
		return nil
	}
	_, err = c.rw.Write(b)
	return err
}

func (c *stdContext) Text(code int, s string) error {
	c.rw.Header().Set("Content-Type", "text/plain; charset=utf-8")
	c.rw.WriteHeader(code)
	if bodyless(code) {
		return nil
	}
	_, err := io.WriteString(&c.rw, s)
	return err
}

func (c *stdContext) NoContent(code int) error {
	c.rw.WriteHeader(code)
	return nil
}

func (c *stdContext) Bytes(code int, b []byte, contentType string) error {
	if contentType == "" {
		contentType = http.DetectContentType(b)
	}
	c.rw.Header().Set("Content-Type", contentType)
	c.rw.WriteHeader(code)
	if bodyless(code) {
		return nil
	}
	_, err := c.rw.Write(b)
	return err
}

func (c *stdContext) DataFromReader(code int, contentType string, r io.Reader, size int) error {
	if rc, ok := r.(io.Closer); ok {
		defer func() { _ = rc.Close() }()
	}
	if contentType != "" {
		c.rw.Header().Set("Content-Type", contentType)
	}
	if size >= 0 {
		c.rw.Header().Set("Content-Length", fmt.Sprintf("%d", size))
	}
	c.rw.WriteHeader(code)
	_, err := io.Copy(&c.rw, r)
	return err
}

func (c *stdContext) File(path string) error {
	http.ServeFile(&c.rw, c.req, path)
	return nil
}

func (c *stdContext) Redirect(code int, location string) error {
	if !httpx.ValidRedirectCode(code) {
		return httpx.NewInternalServerError(fmt.Sprintf("cannot redirect with status code %d", code))
	}
	http.Redirect(&c.rw, c.req, location, code)
	return nil
}

func (c *stdContext) SetHeader(key, value string) {
	c.rw.Header().Set(key, value)
}

func (c *stdContext) SetCookie(cookie *http.Cookie) {
	if cookie != nil {
		http.SetCookie(&c.rw, cookie)
	}
}

// StateStore (httpx.StateStore)

func (c *stdContext) Set(key string, val any) {
	if c.keys == nil {
		c.keys = make(map[string]any, 4)
	}
	c.keys[key] = val
}

func (c *stdContext) Get(key string) (any, bool) {
	// A stored nil reports as absent so every adapter agrees (echo and fiber
	// cannot tell nil from missing).
	val, ok := c.keys[key]
	if !ok || val == nil {
		return nil, false
	}
	return val, true
}

// Context (context.Context accessor + Next)

func (c *stdContext) Context() context.Context { return c.req.Context() }

func (c *stdContext) SetContext(ctx context.Context) {
	c.req = c.req.WithContext(ctx)
}

// Next runs the next layer of the chain and returns its error. A layer that
// returns without calling Next stops the chain, which is how a middleware
// short-circuits a request.
func (c *stdContext) Next() error {
	if c.index < len(c.chain) {
		mw := c.chain[c.index]
		c.index++
		return mw(c)
	}
	if c.index == len(c.chain) {
		c.index++
		if c.leaf != nil {
			return c.leaf(c)
		}
	}
	return nil
}

func (c *stdContext) StatusCode() int { return c.rw.status }

func (c *stdContext) NativeContext() any { return &c.native }

// Flush implements httpx.Flusher: the first flush commits status and headers.
func (c *stdContext) Flush() error {
	if !c.rw.written {
		c.rw.WriteHeader(c.rw.status)
	}
	return http.NewResponseController(c.rw.ResponseWriter).Flush()
}

// Stream implements httpx.Streamer: the response is committed before fn runs
// and every write inside fn reaches the client immediately.
func (c *stdContext) Stream(code int, contentType string, fn func(w io.Writer) error) error {
	if contentType != "" {
		c.rw.Header().Set("Content-Type", contentType)
	}
	c.rw.WriteHeader(code)
	if err := c.Flush(); err != nil {
		return err
	}
	return fn(flushWriter{c: c})
}

type flushWriter struct{ c *stdContext }

func (w flushWriter) Write(p []byte) (int, error) {
	n, err := w.c.rw.Write(p)
	if err != nil {
		return n, err
	}
	return n, w.c.Flush()
}

// responseWriter records whether a response was produced, which is what keeps
// an error from being rendered over it, and what StatusCode reports.
type responseWriter struct {
	http.ResponseWriter
	status  int
	written bool
}

func (w *responseWriter) WriteHeader(code int) {
	if w.written {
		return
	}
	w.status = code
	w.written = true
	w.ResponseWriter.WriteHeader(code)
}

func (w *responseWriter) Write(p []byte) (int, error) {
	if !w.written {
		w.WriteHeader(w.status)
	}
	return w.ResponseWriter.Write(p)
}

// markWritten records a response produced through the underlying writer
// directly, which is how an adapted net/http middleware short-circuits.
func (w *responseWriter) markWritten(status int) {
	if !w.written {
		w.status = status
		w.written = true
	}
}

// Unwrap lets http.ResponseController reach the real writer, so Flush, Hijack
// and deadlines keep working through this wrapper.
func (w *responseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *responseWriter) Flush() {
	if !w.written {
		w.WriteHeader(w.status)
	}
	_ = http.NewResponseController(w.ResponseWriter).Flush()
}

func (w *responseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	conn, rw, err := http.NewResponseController(w.ResponseWriter).Hijack()
	if err == nil {
		// A hijacked connection is a produced response: nothing may render an
		// error body over it afterwards.
		w.written = true
	}
	return conn, rw, err
}

func (w *responseWriter) ReadFrom(r io.Reader) (int64, error) {
	if !w.written {
		w.WriteHeader(w.status)
	}
	if rf, ok := w.ResponseWriter.(io.ReaderFrom); ok {
		return rf.ReadFrom(r)
	}
	return io.Copy(w.ResponseWriter, r)
}

func bodyless(code int) bool {
	return code >= 100 && code < 200 || code == http.StatusNoContent || code == http.StatusNotModified
}

func isMultipart(req *http.Request) bool {
	ct := req.Header.Get("Content-Type")
	return len(ct) >= 9 && (ct[:9] == "multipart" || ct[:9] == "Multipart")
}
