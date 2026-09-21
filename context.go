package httpx

import (
	"context"
	"io"
	"mime/multipart"
	"net/http"
)

// RequestInfo exposes a stable, read-only view of an incoming HTTP request.
//
// All methods are side-effect-free: they MUST NOT consume the request body,
// trigger form parsing, or mutate any internal request state.
type RequestInfo interface {
	Method() string
	Path() string     // Always returns the decoded request path
	FullPath() string // Returns a route pattern when available, empty otherwise

	// ClientIP returns the best-effort client IP.
	//
	// SECURITY: which forwarding headers (X-Forwarded-For, X-Real-IP) are
	// trusted differs per framework by default — gin, echo and hertz trust
	// every peer, fiber trusts none. For a uniform, spoofing-resistant policy use
	// the adapter's WithTrustedProxies option: with it configured,
	// X-Forwarded-For is honored only when the direct peer is inside the
	// given CIDR list, and an empty list ignores forwarding headers
	// entirely. Multi-hop X-Forwarded-For resolution order remains
	// framework-dependent; behind a single proxy layer all adapters agree.
	ClientIP() string

	Param(key string) string
	Params() map[string]string // nil if no params

	Query(key string) string
	Queries() map[string][]string // nil if no queries
	RawQuery() string

	Header(key string) string
	Headers() map[string][]string // nil if no headers

	Cookie(name string) (string, error) // Returns error if cookie not found
	Cookies() map[string]string         // nil if no cookies
}

// BodyAccess provides access to the raw request body.
//
// Methods on BodyAccess MAY consume the request body. Implementations SHOULD
// keep the body readable afterwards so BodyAccess and Binder can coexist.
type BodyAccess interface {
	// BodyRaw returns the full request body as a byte slice.
	//
	// The returned slice belongs to the caller: it stays valid and unchanged
	// after the handler returns, so it can be retained, sent to another
	// goroutine or cached. Adapters that keep the body in a pooled buffer
	// (the fasthttp-based ones) therefore copy it, one allocation the size of
	// the body. Handlers that only need the bytes during the request — large
	// uploads in particular — should prefer BodyReader or the native context,
	// which are the zero-copy paths.
	BodyRaw() ([]byte, error)

	// BodyReader returns a reader for the request body, which the caller may
	// consume. Whether the reader is reusable is implementation-defined.
	BodyReader() io.ReadCloser
}

// FormAccess provides access to form and multipart form data.
//
// Methods on FormAccess MAY trigger form or multipart parsing, which consumes
// the request body, allocates, and can create temporary files on disk. Treat
// them as expensive. Implementations parse at most once per request and reuse
// the result.
type FormAccess interface {
	// FormValue returns the first value associated with the given key, or an
	// empty string when the key is absent.
	//
	// When the same key appears in both the URL query and the form body,
	// which value wins is framework-dependent (gin/echo prefer the body,
	// fiber/hertz prefer the query). Do not rely on the precedence; read
	// Query and the form explicitly when both may be present.
	FormValue(key string) string

	// MultipartForm returns the parsed multipart form. The returned form is
	// owned by the request context and must not be modified by the caller.
	MultipartForm() (*multipart.Form, error)

	// FormFile returns the first file for the provided form field name, or an
	// error when no file is associated with that name.
	FormFile(name string) (*multipart.FileHeader, error)
}

// Request aggregates request inspection and request data access.
//
// RequestInfo methods are side-effect free; BodyAccess and FormAccess methods
// MAY consume the request body or trigger parsing.
type Request interface {
	RequestInfo
	BodyAccess
	FormAccess
}

// Binder decodes parts of the request into a destination struct, driven by
// struct tags.
//
// Bind* decodes and does **not** validate. A `binding` struct tag means
// nothing to httpx; deciding whether a request is acceptable belongs to the
// caller — protovalidate in generated sphere handlers, or whatever check the
// handler writes for itself. Validating inside the binder cannot be
// reintroduced: a struct bound from several sources (BindJSON, BindHeader,
// BindQuery, BindURI) is incomplete until the last call, and no binder can know
// it is the last one to run.
//
// A decode failure is still an error, reported as HTTP 400 via WrapBindError
// on every adapter.
//
// Binder methods MAY consume the request body or trigger parsing.
type Binder interface {
	// BindJSON decodes the JSON request body into dst using `json` tags.
	//
	// Handling of trailing data after the first JSON value is
	// framework-dependent (streaming decoders ignore it, whole-body
	// decoders reject it); do not rely on either behavior.
	BindJSON(dst any) error

	// BindQuery decodes URL query parameters into dst using `query` tags.
	BindQuery(dst any) error

	// BindForm decodes form and multipart form fields into dst using `form`
	// tags, and may trigger form or multipart parsing.
	BindForm(dst any) error

	// BindURI decodes route parameters into dst using `uri` tags.
	//
	// A field tagged `uri:"x"` receives exactly what RequestInfo.Param("x")
	// returns — same string, same percent-decoding — for every route
	// parameter, **including a named wildcard**: /files/*path binds "a/b.txt",
	// not "/a/b.txt" and not "". Adapters that rewrite named wildcards for a
	// router without them (see RouterFeatureNamedWildcard) must resolve the
	// name here too; binding straight off the framework's parameter set
	// fails silently, since an unmatched tag is not an error.
	BindURI(dst any) error

	// BindHeader decodes HTTP headers into dst using `header` tags. Header
	// names are matched case-insensitively.
	BindHeader(dst any) error
}

// Responder writes HTTP responses in a framework-independent manner.
//
// A response is committed once its header has been written, which every
// body-writing method below does. From that point the contract is net/http's,
// measured against a handler writing to an http.ResponseWriter:
//
//   - The status freezes at the value the committing call sent. A later Status,
//     and the code a later body write passes, are both ignored.
//   - The response headers freeze. A later SetHeader or SetCookie is dropped,
//     as is any header a later write would have set, its Content-Type included.
//   - The body appends. Whatever bytes a later write produces are added to what
//     is already there; a call that produces none — NoContent, an empty Bytes —
//     leaves the response as it stands.
//   - None of it is an error. A post-commit write reports success, and no
//     return value tells a caller it wrote into a committed response. Ask
//     ResponseInfo.Committed instead.
//
// Three adapters inherit this from the ResponseWriter they write through; the
// two over buffering frameworks (fiber, hertz) reproduce it, because a buffered
// response would otherwise still be rewritable after the point at which a
// client has conceptually received it.
type Responder interface {
	// Status sets the HTTP status code. It does not write a body, and has no
	// effect — and reports no error — once the response is committed.
	Status(code int)

	// SetHeader sets a response header.
	SetHeader(key, value string)

	// SetCookie adds a Set-Cookie header to the response.
	SetCookie(cookie *http.Cookie)

	// JSON writes v as an "application/json" response and commits it.
	JSON(code int, v any) error

	// Text writes s as a "text/plain; charset=utf-8" response and commits it.
	Text(code int, s string) error

	// NoContent commits the response without writing a body.
	NoContent(code int) error

	// Bytes writes raw bytes with the given Content-Type and commits the
	// response. An empty contentType is replaced by http.DetectContentType
	// sniffing, matching net/http behavior.
	Bytes(code int, b []byte, contentType string) error

	// DataFromReader streams data from r to the response and commits it.
	//
	// size is the total number of bytes to write, or -1 when unknown. It is an
	// int64 so a caller holding a file size or object-store length can pass it
	// through without a truncating conversion.
	//
	// Reader lifecycle: implementations either consume r synchronously before
	// returning (net/http based adapters) or hand it to the framework and
	// consume it after the handler returns (fasthttp/hertz based adapters). If
	// r implements io.Closer it will be closed, but possibly only after the
	// handler has returned. Callers must not reuse or close r themselves.
	DataFromReader(code int, contentType string, r io.Reader, size int64) error

	// File writes the contents of the named file to the response and commits
	// it, using the framework's optimized transfer where available.
	File(path string) error

	// Redirect commits a redirect to location. The code must be a valid
	// redirect status (300-308); implementations return an error for other
	// codes without writing the response.
	Redirect(code int, location string) error
}

// ResponseInfo exposes read-only response state, so middleware can inspect the
// response after downstream handlers have run.
type ResponseInfo interface {
	// StatusCode returns the current response status code.
	StatusCode() int

	// Committed reports whether the response header has been written — exactly
	// the predicate net/http consults before ignoring a WriteHeader call as
	// superfluous. Every body-writing Responder method commits. Status alone
	// does not, on any adapter: it records the code the first write will send.
	//
	// net/http exposes no such accessor on http.ResponseWriter, so this is an
	// addition beyond the standard library rather than a contradiction of it.
	// It exists because the convention layer above httpx needs it and the
	// post-commit rules leave it no other way to find out: a recovery or error
	// middleware deciding whether to render an error body has to know that the
	// handler already sent one, and the write it would make to find out
	// succeeds, appends a second document, and reports nothing.
	Committed() bool
}

// NativeContextProvider exposes the underlying framework context.
//
// This optional capability is an escape hatch for framework-specific features
// that are intentionally not included in the cross-framework Context surface.
type NativeContextProvider interface {
	NativeContext() any
}

// StateStore carries request-scoped values shared across the handler chain.
//
// IMPORTANT: values stored via Set are NOT propagated through the standard
// context.Context returned by Context.Context(). They are visible only to
// middleware and handlers sharing the same httpx.Context instance. To
// propagate a value into downstream business logic, goroutines or RPC calls,
// use SetContext with context.WithValue instead.
//
// Stored values MUST NOT be accessed concurrently without external
// synchronization unless the implementation guarantees concurrency safety.
type StateStore interface {
	// Set associates val with key for the lifetime of the current request,
	// replacing any previous value. Storing a nil value is indistinguishable
	// from absence: Get reports ok=false for it on every adapter.
	Set(key string, val any)

	// Get retrieves the value associated with key. The boolean reports whether
	// the key was present with a non-nil value. Values stored in
	// context.Context are not visible here.
	Get(key string) (any, bool)
}

// Context is the cross-framework surface passed into handlers and middleware.
//
// A Context is valid only for the lifetime of a single request and MUST NOT be
// retained or accessed after the request completes. Do not pass it across
// goroutine boundaries — it may be backed by a pooled object whose lifetime
// ends when the response is sent; pass the standard context.Context from
// Context() instead.
type Context interface {
	Request
	Responder
	Binder
	StateStore
	ResponseInfo

	// Context returns the standard context.Context for the current request,
	// derived from the underlying framework context. It is safe to hand to
	// downstream business logic, database calls or RPC clients.
	//
	// Cancellation is best-effort: on net/http based adapters (gin, echo) the
	// context respects request cancellation and deadlines; on fasthttp/hertz
	// based adapters it may be connection-scoped or context.Background() and
	// may not be canceled when the client disconnects. Do not rely on Done()
	// firing per-request across all frameworks.
	//
	// Values stored via StateStore.Set are NOT visible here; use SetContext
	// with context.WithValue to propagate through the standard context chain.
	Context() context.Context

	// SetContext replaces the standard context.Context for the current
	// request, typically to inject request-scoped metadata:
	//
	//   ctx.SetContext(context.WithValue(ctx.Context(), traceIDKey, id))
	//
	// The provided context should be derived from ctx.Context() to preserve
	// cancellation and deadline propagation.
	SetContext(ctx context.Context)

	// Next is deliberately absent. A Middleware receives the rest of the chain
	// as a Handler and calls it — next(ctx) — so there is nothing for the
	// context to drive, and no per-request layer index for an adapter to carry.
}

// ValidRedirectCode reports whether code is acceptable for Responder.Redirect:
// any redirect status in the 300-308 range. Adapters share this check so an
// invalid code is reported as an error instead of panicking (gin) or being
// silently rewritten to 302 (hertz).
func ValidRedirectCode(code int) bool {
	return code >= http.StatusMultipleChoices && code <= http.StatusPermanentRedirect
}

// AsNativeContext returns the underlying native context when supported.
func AsNativeContext[T any](ctx Context) (T, bool) {
	var zero T
	nativeProvider, ok := ctx.(NativeContextProvider)
	if !ok {
		return zero, false
	}
	native, ok := nativeProvider.NativeContext().(T)
	if !ok {
		return zero, false
	}
	return native, true
}
