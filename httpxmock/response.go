package httpxmock

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/go-sphere/httpx"
)

// Content types written by JSON and Text. They carry the charset because
// that is what the net/http-backed adapters write; the cross-adapter golden
// contracts compare the media type only, so the parameter is this package's
// choice and it follows stdx.
const (
	contentTypeJSON = "application/json; charset=utf-8"
	contentTypeText = "text/plain; charset=utf-8"
)

// WriteKind identifies which Responder method produced a ResponseWrite.
type WriteKind string

// The Responder methods that produce a ResponseWrite.
const (
	KindJSON           WriteKind = "JSON"
	KindText           WriteKind = "Text"
	KindNoContent      WriteKind = "NoContent"
	KindBytes          WriteKind = "Bytes"
	KindDataFromReader WriteKind = "DataFromReader"
	KindFile           WriteKind = "File"
	KindRedirect       WriteKind = "Redirect"
	KindStream         WriteKind = "Stream"
)

// ResponseWrite is one call to a body-writing Responder method, recorded in
// order.
//
// It exists for the assertions the response bytes cannot carry. Two of them
// come up constantly: that a handler answered with NoContent rather than
// happening to write a 204 some other way, and what value a handler handed
// to JSON before it was encoded — a test asserting on a typed envelope wants
// the Go value, not a round trip through JSON that would lose the type and
// pass just as happily on a differently shaped body.
type ResponseWrite struct {
	// Kind is the method that was called.
	Kind WriteKind

	// Code is the status code passed to the call, which is not necessarily
	// the status the response ended up with: a call made after the response
	// committed cannot change it. Compare with Context.StatusCode.
	Code int

	// ContentType is the type the call asked for, before any sniffing. It is
	// empty for the methods that do not take one.
	ContentType string

	// Value is the value handed to JSON, unencoded. It is nil for every
	// other kind.
	Value any

	// Location is the target of a Redirect, Path the argument of a File.
	Location string
	Path     string

	// Body is the bytes this call appended to the response, which is empty
	// when the status forbids a body.
	Body []byte
}

// Responder (httpx.Responder).
//
// The whole of this section models net/http: the status and the response
// headers freeze when the response commits, a later Status or SetHeader is
// silently ignored rather than reported, and a later body write appends. So
// a handler that calls JSON twice leaves the first status and both bodies.
//
// That is what all five adapters do, measured rather than assumed: every one
// answers such a handler with 200, the header set before the first write, and
// the two encodings concatenated.
//
// It was three of the five when this package was written. Three inherit the
// behavior from the ResponseWriter they write through; fiberx and hertzx
// buffer until the handler returns, so nothing had committed while it ran and
// both reported 500 with the late header — fiberx losing the first body
// entirely. They now reproduce the rule rather than expose their buffering,
// pinned by httpxtest's Commit group. net/http was the right model to follow
// either way: it is what the standard library does, and a middleware that
// tries to rewrite an already-answered response has to be correct against it.

// Status sets the status code without writing a body. It has no effect once
// the response has committed.
func (c *Context) Status(code int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.note("Status")
	if !c.res.committed {
		c.res.status = code
	}
}

// SetHeader sets a response header. It has no observable effect once the
// response has committed: the header map is still mutated, exactly as
// net/http's is, but the response was snapshotted at commit and ResponseHeaders
// reports the snapshot.
func (c *Context) SetHeader(key, value string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.note("SetHeader")
	c.res.Header().Set(key, value)
}

// SetCookie appends a Set-Cookie header. A nil cookie is ignored. Like
// SetHeader it has no observable effect after the response commits.
func (c *Context) SetCookie(cookie *http.Cookie) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.note("SetCookie")
	if cookie != nil {
		http.SetCookie(&c.res, cookie)
	}
}

// JSON encodes v as an application/json response and commits it.
//
// A value that cannot be encoded leaves the response completely untouched and
// returns the encoder's error: the marshal happens before anything is
// written, so a failed JSON never half-commits.
func (c *Context) JSON(code int, v any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.note("JSON")

	write := ResponseWrite{Kind: KindJSON, Code: code, ContentType: contentTypeJSON, Value: v}

	if !c.res.committed && !bodyAllowed(code) {
		c.res.Header().Set("Content-Type", contentTypeJSON)
		c.res.WriteHeader(code)
		c.writes = append(c.writes, write)
		return nil
	}
	encoded, err := json.Marshal(v)
	if err != nil {
		return err
	}
	c.res.Header().Set("Content-Type", contentTypeJSON)
	c.res.WriteHeader(code)
	write.Body = c.appendBody(encoded)
	c.writes = append(c.writes, write)
	return nil
}

// Text writes s as a text/plain response and commits it.
func (c *Context) Text(code int, s string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.note("Text")

	write := ResponseWrite{Kind: KindText, Code: code, ContentType: contentTypeText}
	c.res.Header().Set("Content-Type", contentTypeText)
	c.res.WriteHeader(code)
	write.Body = c.appendBody([]byte(s))
	c.writes = append(c.writes, write)
	return nil
}

// NoContent commits the response with no body and no content type.
func (c *Context) NoContent(code int) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.note("NoContent")
	c.res.WriteHeader(code)
	c.writes = append(c.writes, ResponseWrite{Kind: KindNoContent, Code: code})
	return nil
}

// Bytes writes b with the given content type and commits the response. An
// empty contentType is sniffed with http.DetectContentType, as net/http does.
func (c *Context) Bytes(code int, b []byte, contentType string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.note("Bytes")

	if contentType == "" {
		contentType = http.DetectContentType(b)
	}
	write := ResponseWrite{Kind: KindBytes, Code: code, ContentType: contentType}
	c.res.Header().Set("Content-Type", contentType)
	c.res.WriteHeader(code)
	write.Body = c.appendBody(b)
	c.writes = append(c.writes, write)
	return nil
}

// DataFromReader copies r into the response and commits it.
//
// size is the byte count, or -1 when unknown; a non-negative size sets
// Content-Length. r is consumed synchronously and closed when it is an
// io.Closer, so the caller must not reuse or close it.
func (c *Context) DataFromReader(code int, contentType string, r io.Reader, size int64) error {
	c.mu.Lock()
	c.note("DataFromReader")
	if contentType != "" {
		c.res.Header().Set("Content-Type", contentType)
	}
	if size >= 0 {
		c.res.Header().Set("Content-Length", strconv.FormatInt(size, 10))
	}
	c.res.WriteHeader(code)
	start := c.res.body.Len()
	c.mu.Unlock()

	if rc, ok := r.(io.Closer); ok {
		defer func() { _ = rc.Close() }()
	}

	// The lock is released around the copy so a reader that calls back into
	// this Context cannot deadlock; lockedWriter retakes it per chunk. The
	// response is already committed, so nothing the reader does can change
	// the status out from under it.
	var err error
	if r != nil {
		_, err = io.Copy(lockedWriter{c: c}, r)
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	c.writes = append(c.writes, ResponseWrite{
		Kind:        KindDataFromReader,
		Code:        code,
		ContentType: contentType,
		Body:        c.bodySince(start),
	})
	return err
}

// File serves the named file through net/http's http.ServeFile, so Range
// requests, If-Modified-Since and content sniffing behave as they do on a
// real adapter. A missing file is answered with a 404 body rather than
// reported as an error, which is what ServeFile does and what the adapters
// therefore do.
func (c *Context) File(path string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.note("File")
	start := c.res.body.Len()
	http.ServeFile(&c.res, c.req, path)
	c.writes = append(c.writes, ResponseWrite{
		Kind: KindFile,
		Code: c.res.status,
		Path: path,
		Body: c.bodySince(start),
	})
	return nil
}

// Redirect commits a redirect to location. A code outside 300-308 is rejected
// with an error and nothing is written, per httpx.ValidRedirectCode.
func (c *Context) Redirect(code int, location string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.note("Redirect")
	if !httpx.ValidRedirectCode(code) {
		return httpx.NewInternalServerError(fmt.Sprintf("cannot redirect with status code %d", code))
	}
	start := c.res.body.Len()
	http.Redirect(&c.res, c.req, location, code)
	c.writes = append(c.writes, ResponseWrite{
		Kind:     KindRedirect,
		Code:     code,
		Location: location,
		Body:     c.bodySince(start),
	})
	return nil
}

// StatusCode returns the response status: the committed one once the response
// has committed, otherwise the pending one, which starts at 200 and moves with
// Status.
//
// A middleware reading this after next returns sees what the framework would
// send at that moment, which for a request that is about to be answered with
// an error is still 200 — the error has not been rendered yet. That is a real
// footgun on every adapter and this package reproduces it rather than
// smoothing it over.
func (c *Context) StatusCode() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.note("StatusCode")
	return c.res.status
}

// Response read-back. None of these count as a call, so a test can inspect
// the response without disturbing CallCount.

// Committed reports whether the response has been written, which is
// httpx.ResponseInfo.Committed: the response header is out, so the status and
// headers are frozen and a later write appends.
func (c *Context) Committed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.res.committed
}

// Body returns a copy of the response body.
func (c *Context) Body() []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	return bytes.Clone(c.res.body.Bytes())
}

// BodyString returns the response body as a string.
func (c *Context) BodyString() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.res.body.String()
}

// ResponseHeaders returns the headers the response carries: the snapshot
// taken when it committed, or a copy of the pending map before that. Either
// way it is a copy, and mutating it changes nothing.
func (c *Context) ResponseHeaders() http.Header {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.res.sentHeader().Clone()
}

// ResponseHeader returns one response header value, or the empty string.
func (c *Context) ResponseHeader(key string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.res.sentHeader().Get(key)
}

// ResponseCookies returns the cookies the response sets, parsed from its
// Set-Cookie headers.
func (c *Context) ResponseCookies() []*http.Cookie {
	return c.Result().Cookies()
}

// Result assembles the response as an *http.Response, in the spirit of
// httptest.ResponseRecorder.Result, for tests that would rather assert with
// the standard library's helpers than with this package's accessors.
//
// The body is a fresh reader over a copy, so the Context can be read again
// afterwards.
func (c *Context) Result() *http.Response {
	c.mu.Lock()
	defer c.mu.Unlock()
	body := bytes.Clone(c.res.body.Bytes())
	return &http.Response{
		Status:        fmt.Sprintf("%03d %s", c.res.status, http.StatusText(c.res.status)),
		StatusCode:    c.res.status,
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		Header:        c.res.sentHeader().Clone(),
		Body:          io.NopCloser(bytes.NewReader(body)),
		ContentLength: int64(len(body)),
	}
}

// Writes returns every body-writing Responder call in order.
func (c *Context) Writes() []ResponseWrite {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.writes) == 0 {
		return nil
	}
	return append([]ResponseWrite(nil), c.writes...)
}

// LastJSON returns the value most recently handed to JSON, unencoded, and
// whether JSON was called at all. A handler that wrote a typed envelope is
// asserted against with a type assertion on this rather than by decoding the
// body, which would not distinguish two structs with the same JSON shape.
func (c *Context) LastJSON() (any, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i := len(c.writes) - 1; i >= 0; i-- {
		if c.writes[i].Kind == KindJSON {
			return c.writes[i].Value, true
		}
	}
	return nil, false
}

// appendBody writes b and returns what was appended. Callers hold c.mu.
func (c *Context) appendBody(b []byte) []byte {
	start := c.res.body.Len()
	_, _ = c.res.Write(b)
	return c.bodySince(start)
}

// bodySince returns a copy of everything added to the body since start.
// Callers hold c.mu.
func (c *Context) bodySince(start int) []byte {
	body := c.res.body.Bytes()
	if start >= len(body) {
		return nil
	}
	return bytes.Clone(body[start:])
}

// lockedWriter appends to the response, taking the lock for each write. It is
// what the Context hands to code that may run arbitrary callbacks — the
// io.Copy in DataFromReader, the callback in Stream — so those never hold the
// lock across code they do not control.
type lockedWriter struct{ c *Context }

func (w lockedWriter) Write(p []byte) (int, error) {
	w.c.mu.Lock()
	defer w.c.mu.Unlock()
	return w.c.res.Write(p)
}

// recorder is the response under construction. It is an http.ResponseWriter
// so http.ServeFile and http.Redirect can write through it, which is how File
// and Redirect get real net/http behavior instead of an approximation of it.
//
// It models net/http's commit rules directly: header and status are captured
// at commit, a second WriteHeader is ignored, and later writes append.
type recorder struct {
	header   http.Header
	snapshot http.Header

	status    int
	body      bytes.Buffer
	committed bool

	// flushes is the body length at each flush, which is what makes an
	// incremental response testable: a stream that sent three events in one
	// write and one that sent three flushed events produce the same bytes and
	// differ only here.
	flushes []int
}

func (r *recorder) Header() http.Header {
	if r.header == nil {
		r.header = make(http.Header)
	}
	return r.header
}

// WriteHeader commits the response. A 1xx status other than 101 is
// informational: net/http sends it and keeps the response open, so it neither
// commits nor changes the status here.
func (r *recorder) WriteHeader(code int) {
	if r.committed {
		return
	}
	if code >= 100 && code < 200 && code != http.StatusSwitchingProtocols {
		return
	}
	r.status = code
	r.committed = true
	r.snapshot = r.Header().Clone()
}

func (r *recorder) Write(p []byte) (int, error) {
	if !r.committed {
		r.WriteHeader(r.status)
	}
	if !r.committed {
		// The pending status was informational, so committing on it was a
		// no-op. A body forces a real response; net/http answers 200.
		r.WriteHeader(http.StatusOK)
	}
	if !bodyAllowed(r.status) {
		// 204 and 304 carry no body on the wire. Reporting the bytes as
		// written rather than erroring matches what a caller observes from
		// the adapters, none of which surfaces an error here.
		return len(p), nil
	}
	return r.body.Write(p)
}

// Flush implements http.Flusher and records the boundary. There is no
// connection behind this recorder, so there is nothing to push; the boundary
// is the entire observable effect.
func (r *recorder) Flush() {
	if !r.committed {
		r.WriteHeader(r.status)
	}
	r.flushes = append(r.flushes, r.body.Len())
}

// sentHeader is what the response carries: the snapshot once committed, the
// live map before that. Callers hold c.mu.
func (r *recorder) sentHeader() http.Header {
	if r.committed {
		return r.snapshot
	}
	return r.Header()
}

// bodyAllowed reports whether a response with this status may carry a body.
func bodyAllowed(status int) bool {
	switch {
	case status >= 100 && status < 200:
		return false
	case status == http.StatusNoContent, status == http.StatusNotModified:
		return false
	default:
		return true
	}
}
