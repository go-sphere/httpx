// Package httpxmock provides a complete in-memory httpx.Context for unit
// tests of middleware and handlers.
//
// It exists because of what test authors reach for otherwise: a struct that
// embeds httpx.Context and overrides the two or three methods the test needs.
// The embedded nil interface fills out the method set so the type compiles,
// and every method the code under test touches but the test did not
// anticipate panics with a nil dereference. Seventeen such fakes had
// accumulated across the sphere repository, each a slightly different subset
// of the same surface, several of them duplicated a second time with
// sync/atomic fields so a stress test could share one. This package is the
// one implementation they collapse into.
//
// The rule that makes it worth having is that nothing here is left unfilled:
// every method of httpx.Context has a real body, so a method the test never
// thought about returns a sane zero value instead of taking the test process
// down. That is the entire point — a mock whose unexercised corners panic is
// only marginally better than no mock at all, because it turns "the code now
// reads one more header" into a crash rather than a passing test.
//
// # What it is not
//
// This is a unit-test double, not an adapter. It is not in the conformance
// suite (httpxtest), which exists for adapter authors and drives real
// engines; a downstream consumer testing its own middleware should not have
// to depend on that machinery. It implements no router, so FullPath and Param
// report whatever the test configured and nothing else, and it deliberately
// does not implement httpx.NativeContextProvider — there is no native context
// to hand out, and httpx.AsNativeContext correctly reports false. The Binder
// methods all return ErrBindUnsupported; see bind.go for why.
//
// Anything that depends on real routing or real decoding — what a router
// reports as FullPath for a wildcard route, what go-playground/form does to a
// query string — must be tested against a real engine. stdx is the cheapest
// one: it needs no framework and its Engine is itself an http.Handler.
//
// # Usage
//
//	ctx := httpxmock.NewRequest(http.MethodGet, "/users/42", nil,
//		httpxmock.WithFullPath("/users/:id"),
//		httpxmock.WithParam("id", "42"),
//	)
//	next := &httpxmock.Handler{}
//	err := httpxmock.Run(ctx, next.Handle, mw)
//	if !next.Called() {
//		t.Fatalf("the layer stopped the chain: %d %s", ctx.StatusCode(), ctx.BodyString())
//	}
//
// Run and Handler are the second half of the package: a middleware cannot be
// driven by a Context alone, it needs something at the bottom of the chain,
// and "did the request get past this layer" is the first thing every such
// test asks. See Run for the four downstream copies of those three lines.
//
// The Context is both the thing under test and the recorder: response state
// is read back off the same object that was passed in. A separate recorder
// in the spirit of httptest.ResponseRecorder was considered and rejected,
// because a middleware only ever receives the Context — anything it can
// observe about the response, it observes through ResponseInfo on that
// value — so splitting the two would mean handing tests a second object that
// exists only to be read. Every one of the fakes this replaces already read
// its response state off the fake itself.
//
// # Concurrency
//
// A Context is safe for concurrent use. That is deliberately stricter than
// the real adapters, whose contexts are single-goroutine and may be pooled,
// and it is not a statement about them: it is here so that a stress test can
// share one Context without the test author writing a second, atomic copy of
// the type, which is exactly what happened three times downstream. Code that
// passes a real httpx.Context between goroutines is still wrong.
package httpxmock

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"

	"github.com/go-sphere/httpx"
)

// The full Context surface plus the two optional capabilities that can be
// honestly implemented without a connection. NativeContextProvider is
// deliberately absent; see the package doc.
var (
	_ httpx.Context  = (*Context)(nil)
	_ httpx.Flusher  = (*Context)(nil)
	_ httpx.Streamer = (*Context)(nil)
)

// Context is an in-memory httpx.Context backed by an *http.Request and a
// response recorder.
//
// Use New or NewRequest to build one; the zero value is not usable because it
// has no request to read from.
type Context struct {
	// mu guards every field below it. Exported methods take it and call
	// unexported helpers that never take it again, so the lock is never
	// re-entered. The two places that hand a writer to caller code — Stream
	// and DataFromReader — release it before doing so.
	mu sync.Mutex

	req      *http.Request
	fullPath string
	params   map[string]string
	// clientIP overrides the peer address. Empty means "derive from
	// RemoteAddr", which is what an adapter with no trusted-proxy policy
	// configured does: never believe a forwarding header we cannot attribute
	// to a trusted hop.
	clientIP string

	state map[string]any
	calls map[string]int

	res    recorder
	writes []ResponseWrite
}

// New wraps req as a Context. A nil req is replaced by a GET "/" request, so
// httpxmock.New(nil) is a usable context for a test that cares only about the
// response side.
//
// The request is taken as-is and is not copied: set anything the options do
// not cover — RemoteAddr, TLS, Host — on req before calling New.
func New(req *http.Request, opts ...Option) *Context {
	if req == nil {
		req = httptest.NewRequest(http.MethodGet, "/", nil)
	}
	c := &Context{req: req}
	c.res.status = http.StatusOK
	for _, opt := range opts {
		if opt != nil {
			opt(c)
		}
	}
	return c
}

// NewRequest builds a request with httptest.NewRequest and wraps it. It is
// the common case in one call:
//
//	ctx := httpxmock.NewRequest(http.MethodPost, "/upload", body)
//
// target follows httptest.NewRequest's rules — a path, or a full URL — and an
// unparseable one panics there, not here.
func NewRequest(method, target string, body io.Reader, opts ...Option) *Context {
	return New(httptest.NewRequest(method, target, body), opts...)
}

// Option configures a Context at construction. Options are applied in order.
type Option func(*Context)

// WithFullPath sets the route pattern FullPath reports.
//
// There is no router here to derive it from, so a test exercising a layer
// that keys on FullPath — operation-based authorization, per-route rate
// limiting — has to state the pattern it is standing in for. An unset
// FullPath is the empty string, which is what every adapter reports for a
// path no route matched.
func WithFullPath(pattern string) Option {
	return func(c *Context) { c.fullPath = pattern }
}

// WithParam sets one route parameter. Repeatable.
func WithParam(key, value string) Option {
	return func(c *Context) {
		if c.params == nil {
			c.params = make(map[string]string)
		}
		c.params[key] = value
	}
}

// WithParams sets the route parameters, merging into anything already set.
//
// Values are exactly what Param returns: a named wildcard /files/*path binds
// "a/b.txt", with no leading slash, matching the BindURI contract.
func WithParams(params map[string]string) Option {
	return func(c *Context) {
		if len(params) == 0 {
			return
		}
		if c.params == nil {
			c.params = make(map[string]string, len(params))
		}
		for k, v := range params {
			c.params[k] = v
		}
	}
}

// WithClientIP overrides what ClientIP reports. Without it ClientIP is the
// host part of the request's RemoteAddr, which httptest.NewRequest sets to
// 192.0.2.1.
func WithClientIP(ip string) Option {
	return func(c *Context) { c.clientIP = ip }
}

// WithContext sets the standard context.Context for the request, which is
// how a test supplies a deadline, a cancellation, or a seeded value:
//
//	ctx, cancel := context.WithCancel(t.Context())
//	c := httpxmock.New(nil, httpxmock.WithContext(ctx))
func WithContext(ctx context.Context) Option {
	return func(c *Context) {
		if ctx != nil {
			c.req = c.req.WithContext(ctx)
		}
	}
}

// WithState seeds one StateStore entry, as if a middleware above had set it.
// Repeatable. A nil value is stored but reads back as absent, per the
// StateStore contract.
func WithState(key string, val any) Option {
	return func(c *Context) { c.setState(key, val) }
}

// WithHeader adds a request header.
func WithHeader(key, value string) Option {
	return func(c *Context) { c.req.Header.Add(key, value) }
}

// WithCookie adds a request cookie.
func WithCookie(cookie *http.Cookie) Option {
	return func(c *Context) {
		if cookie != nil {
			c.req.AddCookie(cookie)
		}
	}
}

// Request returns the underlying *http.Request, for a test that needs to
// reach past the httpx surface.
//
// SetContext replaces the request the same way net/http does, with
// Request.WithContext, so a pointer captured before a SetContext call is
// stale. Call Request again rather than holding one.
func (c *Context) Request() *http.Request {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.req
}

// Context returns the standard context.Context carried by the request.
// Values stored with Set are not visible here; that separation is a contract
// of httpx.StateStore and is asserted in this package's tests.
func (c *Context) Context() context.Context {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.note("Context")
	return c.req.Context()
}

// SetContext replaces the standard context.Context, by replacing the request
// with one carrying ctx. A nil ctx is ignored rather than stored, because
// context.Context's own contract forbids a nil and the panic would surface
// far from the call that caused it.
func (c *Context) SetContext(ctx context.Context) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.note("SetContext")
	if ctx == nil {
		return
	}
	c.req = c.req.WithContext(ctx)
}

// Set stores a request-scoped value. Storing nil is indistinguishable from
// absence: Get reports ok=false for it, which is what every adapter does.
func (c *Context) Set(key string, val any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.note("Set")
	c.setState(key, val)
}

// Get reads a request-scoped value. ok is false for a key that was never set
// and for one set to nil.
func (c *Context) Get(key string) (any, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.note("Get")
	val, ok := c.state[key]
	if !ok || val == nil {
		return nil, false
	}
	return val, true
}

func (c *Context) setState(key string, val any) {
	if c.state == nil {
		c.state = make(map[string]any, 4)
	}
	c.state[key] = val
}

// CallCount reports how many times the named method was called. The name is
// the Go method name exactly: "Method", "FullPath", "Header", "JSON".
//
// It is here for the one assertion a recorder cannot make any other way:
// that a cheap check short-circuited before an expensive one ran. A matcher
// that compares the request method first and the route pattern second should
// leave FullPath at zero when the method already missed, and there is nothing
// else to observe about that.
func (c *Context) CallCount(name string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls[name]
}

// Calls returns a copy of every call count recorded so far.
func (c *Context) Calls() map[string]int {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.calls) == 0 {
		return nil
	}
	out := make(map[string]int, len(c.calls))
	for k, v := range c.calls {
		out[k] = v
	}
	return out
}

// note records a call. Callers hold c.mu.
func (c *Context) note(name string) {
	if c.calls == nil {
		c.calls = make(map[string]int, 8)
	}
	c.calls[name]++
}

// hostOf returns the host part of a "host:port" address, or addr unchanged
// when it carries no port — which is what an IPv6 literal without brackets
// looks like, and what a test that set RemoteAddr by hand usually wrote.
func hostOf(addr string) string {
	if addr == "" {
		return ""
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	return host
}
