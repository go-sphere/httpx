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
	"strconv"

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

	// keys survives across requests: it is cleared, not dropped, when the
	// context is recycled, so a request that stores state does not allocate
	// the map again.
	keys map[string]any

	// enc is bound to &c.rw for the pool's lifetime; JSON reuses it so a
	// response never copies the encoded bytes out of the encoder's buffer.
	enc *json.Encoder

	// query caches URL.Query(); queryRaw is the RawQuery it was parsed from.
	// The cache is only ever read, so it needs no reset: a later request (or
	// a request swapped through Native) with a different query string misses
	// on queryRaw, and one with an equal query string is served an equal map.
	query    url.Values
	queryRaw string

	// uriValues is the url.Values handed to the uri decoder, reused across
	// requests; uriBuf backs its single-element slices.
	uriValues url.Values
	uriBuf    [8][1]string
}

// reset prepares a pooled context for one request. engine and native are set
// once when the context is created: both are constant for the pool's lifetime,
// so writing them per request would be pure overhead.
func (c *stdContext) reset(w http.ResponseWriter, req *http.Request) {
	c.rw = responseWriter{ResponseWriter: w, status: http.StatusOK}
	c.req = req
	c.route = nil
	c.values = c.valueBuf[:0]
	c.chain = nil
	c.leaf = nil
	c.index = 0
}

// maxRetainedKeys bounds the state map a recycled context keeps: a request
// that stored an unusual number of keys should not pin that map for every
// request that follows.
const maxRetainedKeys = 64

// recycle releases per-request state before the context returns to the pool.
func (c *stdContext) recycle() {
	switch n := len(c.keys); {
	case n == 0:
	case n > maxRetainedKeys:
		c.keys = nil
	default:
		clear(c.keys)
	}
}

// Native is what AsNativeContext yields on this adapter: the plain net/http
// pair this adapter runs on. Replacing the request (or the writer) here is
// visible to the rest of the chain, which is what lets AdaptStdMiddleware
// forward request mutations and writer wrapping.
type Native struct {
	c *stdContext
}

func (n *Native) Request() *http.Request              { return n.c.req }
func (n *Native) SetRequest(req *http.Request)        { n.c.req = req }
func (n *Native) ResponseWriter() http.ResponseWriter { return &n.c.rw }
func (n *Native) SetWriter(w http.ResponseWriter) {
	n.c.rw.ResponseWriter = w
	// A wrapping writer may own a different header map.
	n.c.rw.header = nil
}
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

// queryValues parses the query string once per request. A request without a
// query string yields nil, which every reader below handles.
func (c *stdContext) queryValues() url.Values {
	raw := c.req.URL.RawQuery
	if raw == "" {
		return nil
	}
	if c.query == nil || c.queryRaw != raw {
		c.query = c.req.URL.Query()
		c.queryRaw = raw
	}
	return c.query
}

func (c *stdContext) Query(key string) string {
	return c.queryValues().Get(key)
}

func (c *stdContext) Queries() map[string][]string {
	queries := c.queryValues()
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
	body, err := readBody(c.req.Body, c.req.ContentLength)
	if err != nil {
		return nil, err
	}
	_ = c.req.Body.Close()
	// Restore the body so a binder running after this still sees it. The
	// returned slice is a fresh copy the caller owns (BodyAccess contract).
	c.req.Body = io.NopCloser(bytes.NewReader(body))
	return body, nil
}

// maxBodyPrealloc caps what readBody allocates on the strength of
// Content-Length alone: a client may declare far more than it sends.
const maxBodyPrealloc = 4 << 20

// readBody reads r to EOF. With a trustworthy size it allocates once, where
// io.ReadAll would grow through a dozen buffers and copy the body twice.
func readBody(r io.Reader, size int64) ([]byte, error) {
	if size < 0 || size > maxBodyPrealloc {
		return io.ReadAll(r)
	}
	// One byte of slack lets the final Read report EOF without regrowing.
	buf := make([]byte, 0, size+1)
	for {
		if len(buf) == cap(buf) {
			// More than declared: net/http truncates at Content-Length, but a
			// Body installed by middleware need not.
			rest, err := io.ReadAll(r)
			return append(buf, rest...), err
		}
		n, err := r.Read(buf[len(buf):cap(buf)])
		buf = buf[:len(buf)+n]
		if err == io.EOF {
			return buf, nil
		}
		if err != nil {
			return buf, err
		}
	}
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
	// The body is read whole and unmarshaled in place: a json.Decoder costs
	// three allocations before it has read a byte.
	body, err := readBody(c.req.Body, c.req.ContentLength)
	if err != nil {
		return httpx.WrapBindError(err)
	}
	if len(body) == 0 {
		// What a Decoder reports for an empty body.
		return httpx.WrapBindError(io.EOF)
	}
	if err := json.Unmarshal(body, dst); err != nil {
		return httpx.WrapBindError(err)
	}
	return httpx.WrapBindError(validateStruct(dst))
}

func (c *stdContext) BindQuery(dst any) error {
	if err := queryDecoder.Decode(dst, c.queryValues()); err != nil {
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
		values := c.uriValues
		if values == nil {
			values = make(url.Values, len(c.uriBuf))
			c.uriValues = values
		}
		for i, name := range c.route.params {
			if i >= len(c.values) {
				break
			}
			if i < len(c.uriBuf) {
				c.uriBuf[i][0] = c.values[i]
				values[name] = c.uriBuf[i][:]
			} else {
				values[name] = []string{c.values[i]}
			}
		}
		err := uriDecoder.Decode(dst, values)
		clear(values)
		if err != nil {
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

// Content-Type values are stored as ready-made slices: assigning one into the
// header map is what gin does too, and it is the difference between zero and
// one allocation per response. append on a full slice always reallocates, so
// a later Header.Add cannot write into the shared value.
var (
	jsonContentType = []string{"application/json; charset=utf-8"}
	textContentType = []string{"text/plain; charset=utf-8"}
)

func (c *stdContext) JSON(code int, v any) error {
	if bodyless(code) {
		c.rw.Header()["Content-Type"] = jsonContentType
		c.rw.WriteHeader(code)
		return nil
	}
	if !c.rw.written {
		c.rw.status = code
	}
	if c.enc == nil {
		c.enc = json.NewEncoder((*jsonBodyWriter)(&c.rw))
	}
	// Encode marshals fully before its single Write, so a value that cannot
	// be encoded leaves the response untouched — exactly as Marshal did.
	if err := c.enc.Encode(v); err != nil {
		// A failed Write is sticky on the encoder; drop it so the recycled
		// context is not poisoned for the next request.
		c.enc = nil
		return err
	}
	return nil
}

// jsonBodyWriter is the sink the pooled encoder writes to. It sets the
// content type on the way through, and drops the '\n' json.Encoder appends
// after every value: compact JSON never contains a raw newline, so the last
// byte of a chunk being one means it is the terminator.
type jsonBodyWriter responseWriter

func (w *jsonBodyWriter) Write(p []byte) (int, error) {
	n := len(p)
	if n > 0 && p[n-1] == '\n' {
		p = p[:n-1]
	}
	rw := (*responseWriter)(w)
	rw.Header()["Content-Type"] = jsonContentType
	if _, err := rw.Write(p); err != nil {
		return 0, err
	}
	return n, nil
}

func (c *stdContext) Text(code int, s string) error {
	c.rw.Header()["Content-Type"] = textContentType
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
		c.rw.Header().Set("Content-Length", strconv.Itoa(size))
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
		c.keys = make(map[string]any, 8)
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
	// header caches the underlying writer's map until the response is
	// committed. Before that point the real Header() has no side effect, so
	// the cache is exact; after it, net/http snapshots the map on access so
	// later changes are not sent, and the call goes through again to keep
	// that. http.ServeFile alone reads or writes the header a dozen times.
	header  http.Header
	status  int
	written bool
}

func (w *responseWriter) Header() http.Header {
	if w.written {
		return w.ResponseWriter.Header()
	}
	if w.header == nil {
		w.header = w.ResponseWriter.Header()
	}
	return w.header
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
