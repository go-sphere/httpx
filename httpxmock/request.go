package httpxmock

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
)

// defaultMultipartMemory matches every adapter: parse up to 32 MiB of a
// multipart form in memory and spill the rest to temporary files.
const defaultMultipartMemory = 32 << 20

// Request info (httpx.RequestInfo). Every method here is side-effect free, as
// the interface requires: none of them reads the body or triggers parsing.

// Method returns the request method.
func (c *Context) Method() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.note("Method")
	return c.req.Method
}

// Path returns the decoded request path.
func (c *Context) Path() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.note("Path")
	return c.req.URL.Path
}

// FullPath returns the route pattern supplied with WithFullPath, or the empty
// string when none was — which is what an adapter reports for a path no route
// matched.
func (c *Context) FullPath() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.note("FullPath")
	return c.fullPath
}

// ClientIP returns the IP supplied with WithClientIP, or the host part of the
// request's RemoteAddr.
//
// Forwarding headers are never consulted. That is the behavior of an adapter
// whose trusted-proxy list is configured and empty, and the only honest
// default here: there is no peer to check a proxy policy against, so believing
// X-Forwarded-For would mean believing anything the test's own request said.
// A test that wants the resolved-through-a-proxy answer should state it with
// WithClientIP.
func (c *Context) ClientIP() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.note("ClientIP")
	if c.clientIP != "" {
		return c.clientIP
	}
	return hostOf(c.req.RemoteAddr)
}

// Param returns the route parameter, or the empty string when unset.
func (c *Context) Param(key string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.note("Param")
	return c.params[key]
}

// Params returns a copy of the route parameters, or nil when there are none.
// The copy is what keeps a handler from reaching back into the Context's own
// map, which is what every adapter guarantees too.
func (c *Context) Params() map[string]string {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.note("Params")
	if len(c.params) == 0 {
		return nil
	}
	out := make(map[string]string, len(c.params))
	for k, v := range c.params {
		out[k] = v
	}
	return out
}

// Query returns the first value for the query key.
func (c *Context) Query(key string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.note("Query")
	return c.req.URL.Query().Get(key)
}

// Queries returns a copy of the parsed query string, or nil when empty.
func (c *Context) Queries() map[string][]string {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.note("Queries")
	values := c.req.URL.Query()
	if len(values) == 0 {
		return nil
	}
	out := make(map[string][]string, len(values))
	for k, v := range values {
		out[k] = append([]string(nil), v...)
	}
	return out
}

// RawQuery returns the undecoded query string.
func (c *Context) RawQuery() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.note("RawQuery")
	return c.req.URL.RawQuery
}

// Header returns the first value for a request header, matched
// case-insensitively through net/http's canonicalization.
func (c *Context) Header(key string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.note("Header")
	return c.req.Header.Get(key)
}

// Headers returns a copy of the request headers keyed canonically, or nil
// when there are none. Host is not among them: net/http carries it on
// Request.Host, not in the header map, and the adapters agree.
func (c *Context) Headers() map[string][]string {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.note("Headers")
	if len(c.req.Header) == 0 {
		return nil
	}
	out := make(map[string][]string, len(c.req.Header))
	for k, v := range c.req.Header {
		out[textproto.CanonicalMIMEHeaderKey(k)] = append([]string(nil), v...)
	}
	return out
}

// Cookie returns a request cookie's value, or http.ErrNoCookie when it is
// absent — the same error every adapter reports, so a test can compare with
// errors.Is rather than against a string.
func (c *Context) Cookie(name string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.note("Cookie")
	cookie, err := c.req.Cookie(name)
	if err != nil {
		return "", http.ErrNoCookie
	}
	return cookie.Value, nil
}

// Cookies returns the request cookies as a name/value map, or nil when there
// are none.
func (c *Context) Cookies() map[string]string {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.note("Cookies")
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

// Body and form access (httpx.BodyAccess, httpx.FormAccess). Unlike the
// methods above, these may consume the body and trigger parsing.

// FormValue returns the first value for the key from the query string or the
// form body, via net/http's own precedence.
func (c *Context) FormValue(key string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.note("FormValue")
	return c.req.FormValue(key)
}

// MultipartForm parses the request as a multipart form and returns it. The
// form belongs to the request and must not be modified.
func (c *Context) MultipartForm() (*multipart.Form, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.note("MultipartForm")
	if err := c.parseMultipart(); err != nil {
		return nil, err
	}
	return c.req.MultipartForm, nil
}

// FormFile returns the first uploaded file for the field name.
//
// It closes the multipart.File that net/http opens and returns only the
// header, matching every adapter: the header is what a caller needs, and it
// can be reopened as often as the test likes.
func (c *Context) FormFile(name string) (*multipart.FileHeader, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.note("FormFile")
	if err := c.parseMultipart(); err != nil {
		return nil, err
	}
	file, header, err := c.req.FormFile(name)
	if err != nil {
		return nil, err
	}
	_ = file.Close()
	return header, nil
}

// parseMultipart parses the multipart body at most once. Callers hold c.mu.
func (c *Context) parseMultipart() error {
	if c.req.MultipartForm != nil {
		return nil
	}
	return c.req.ParseMultipartForm(defaultMultipartMemory)
}

// BodyRaw returns the whole request body.
//
// The returned slice belongs to the caller, per httpx.BodyAccess, and the
// body is restored afterwards so a later read still sees it. A request with
// no body yields nil and no error.
func (c *Context) BodyRaw() ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.note("BodyRaw")
	if c.req.Body == nil {
		return nil, nil
	}
	body, err := io.ReadAll(c.req.Body)
	if err != nil {
		return nil, err
	}
	_ = c.req.Body.Close()
	// The restored body is a separate copy, so a caller that mutates what it
	// was given cannot corrupt what the next read sees. The BodyAccess
	// contract promises the returned slice belongs to the caller; a shared
	// backing array would make that true only until somebody wrote to it.
	c.req.Body = io.NopCloser(bytes.NewReader(bytes.Clone(body)))
	return body, nil
}

// BodyReader returns the request body, or http.NoBody when there is none.
// It is not rewound: reading it consumes the body.
func (c *Context) BodyReader() io.ReadCloser {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.note("BodyReader")
	if c.req.Body != nil {
		return c.req.Body
	}
	return http.NoBody
}
