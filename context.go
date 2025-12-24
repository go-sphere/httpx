package httpx

import (
	"context"
	"io"
	"mime/multipart"
	"net/http"
)

type H map[string]any

// RequestInfo exposes a stable, read-only view of an incoming HTTP request.
//
// All methods on RequestInfo are side effect free:
// calling them MUST NOT consume the request body, trigger form parsing,
// or mutate any internal request state.
//
// This interface is intended to be safely used by middleware and handlers
// that only need request metadata such as routing, headers, queries, and cookies.
//
// Implementations should provide best-effort, framework-independent behavior
// across supported HTTP frameworks.
type RequestInfo interface {
	Method() string
	Path() string     // Always returns request path
	FullPath() string // Returns route pattern when available, empty otherwise
	ClientIP() string // Best-effort client IP detection

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
// Methods on BodyAccess MAY consume the request body.
// Implementations SHOULD ensure the body can be read multiple times
// when possible, so that BodyAccess and Binder can coexist safely.
//
// Callers must assume that reading from the body has consumption semantics
// and should avoid invoking these methods unless necessary.
type BodyAccess interface {
	// BodyRaw returns the full request body as a byte slice.
	//
	// Calling this method may consume the underlying request body.
	// Implementations should make best-effort to allow subsequent reads.
	BodyRaw() ([]byte, error)

	// BodyReader returns a reader for the request body.
	//
	// The returned reader may be consumed by the caller.
	// Implementations should document whether the reader is reusable.
	BodyReader() io.ReadCloser
}

// FormAccess provides access to form and multipart form data.
//
// Methods on FormAccess MAY trigger form or multipart parsing.
// Parsing form data can have observable side effects, including:
//   - consuming the request body
//   - allocating memory
//   - creating temporary files on disk
//
// Callers should treat these methods as potentially expensive and
// avoid calling them unless form data is required.
//
// Implementations should ensure that form parsing is performed at most once
// per request and that parsed results are reused across calls.
type FormAccess interface {
	// FormValue returns the first value associated with the given key.
	//
	// Calling this method may trigger form parsing.
	// If the key does not exist, an empty string is returned.
	FormValue(key string) string

	// MultipartForm returns the parsed multipart form.
	//
	// Calling this method may trigger multipart parsing and cause
	// temporary files to be created on disk.
	//
	// The returned *multipart.Form is owned by the request context and
	// must not be modified by the caller.
	MultipartForm() (*multipart.Form, error)

	// FormFile returns the first file for the provided form field name.
	//
	// Calling this method may trigger multipart parsing.
	// If no file is associated with the given name, an error is returned.
	FormFile(name string) (*multipart.FileHeader, error)
}

// Request aggregates request inspection and request data access capabilities.
//
// Request is a composite interface that combines side-effect-free request
// metadata access with request body and form access.
//
// Callers should be aware that while RequestInfo methods are guaranteed
// to be side-effect free, methods provided by BodyAccess and FormAccess
// MAY consume the request body or trigger request parsing.
type Request interface {
	// RequestInfo provides read-only access to request metadata.
	RequestInfo

	// BodyAccess provides access to the raw request body and may
	// have consumption semantics.
	BodyAccess

	// FormAccess provides access to form and multipart form data and
	// may trigger parsing with observable side effects.
	FormAccess
}

// Binder standardizes payload decoding across HTTP frameworks.
//
// Binder methods decode data from different parts of the request
// into the provided destination structure. Decoding behavior is
// based on struct tags and follows framework-independent conventions.
//
// Binder methods MAY consume the request body or trigger parsing
// of request data. Implementations SHOULD ensure that decoding can
// coexist safely with BodyAccess and FormAccess when possible.
type Binder interface {
	// BindJSON decodes the JSON request body into dst.
	//
	// Decoding is performed based on `json` struct tags.
	//
	// Calling this method may consume the request body.
	// Implementations should make best-effort to allow the body
	// to be read again after binding.
	BindJSON(dst any) error

	// BindQuery decodes URL query parameters into dst.
	//
	// Decoding is performed based on `query` struct tags.
	BindQuery(dst any) error

	// BindForm decodes form and multipart form fields into dst.
	//
	// Decoding is performed based on `form` struct tags.
	// Calling this method may trigger form or multipart parsing.
	BindForm(dst any) error

	// BindURI decodes route parameters into dst.
	//
	// Decoding is performed based on `uri` struct tags.
	BindURI(dst any) error

	// BindHeader decodes HTTP headers into dst.
	//
	// Decoding is performed based on `header` struct tags.
	// Header field names should be treated in a case-insensitive manner.
	BindHeader(dst any) error
}

// Responder writes HTTP responses in a framework-independent manner.
//
// Methods on Responder mutate the outgoing response and return errors
// to indicate success or failure of response operations.
// Once a response body is written, the response is considered committed,
// and further attempts to modify status code or headers may return errors,
// depending on the underlying framework.
//
// Implementations should ensure consistent behavior across frameworks
// where possible.
type Responder interface {
	// Status sets the HTTP status code for the response.
	//
	// Calling this method does not write the response body.
	Status(code int)

	// SetHeader sets a response header.
	//
	// This method does not write the response body.
	SetHeader(key, value string)

	// SetCookie adds a Set-Cookie header to the response.
	//
	// This method does not write the response body.
	SetCookie(cookie *http.Cookie)

	// JSON writes the given value as a JSON response with the provided status code.
	//
	// The Content-Type header should be set to "application/json".
	// Calling this method commits the response.
	// Returns nil on success, error on failure (e.g., JSON marshaling error,
	// response already committed).
	JSON(code int, v any) error

	// Text writes the given string as a plain text response with the provided status code.
	//
	// The Content-Type header should be set to "text/plain; charset=utf-8".
	// Calling this method commits the response.
	// Returns nil on success, error on failure (e.g., response already committed).
	Text(code int, s string) error

	// NoContent writes a response with no body and the provided status code.
	//
	// This method commits the response without writing a body.
	// Returns nil on success, error on failure (e.g., response already committed).
	NoContent(code int) error

	// Bytes writes raw bytes to the response with the provided status code
	// and Content-Type.
	//
	// Calling this method commits the response.
	// Returns nil on success, error on failure (e.g., response already committed).
	Bytes(code int, b []byte, contentType string) error

	// DataFromReader streams data from the provided reader to the response.
	//
	// The size parameter specifies the total number of bytes to be written.
	// A size of -1 indicates that the size is unknown.
	//
	// Calling this method commits the response.
	// Returns nil on success, error on failure (e.g., IO error, response already committed).
	DataFromReader(code int, contentType string, r io.Reader, size int) error

	// File writes the contents of the specified file to the response.
	//
	// Implementations may use optimized file transfer mechanisms
	// provided by the underlying framework.
	// Calling this method commits the response.
	// Returns nil on success, error on failure (e.g., file not found, response already committed).
	File(path string) error

	// Redirect sends a redirect response to the client with the provided
	// status code and target location.
	//
	// Calling this method commits the response.
	// Returns nil on success, error on failure (e.g., invalid status code, response already committed).
	Redirect(code int, location string) error
}

// StateStore carries request-scoped values.
//
// StateStore provides a simple key-value storage that is scoped to the
// lifetime of a single request. Values stored in StateStore are intended
// to be shared between middleware and handlers handling the same request.
//
// Stored values MUST NOT be accessed concurrently without external
// synchronization unless the implementation explicitly guarantees
// concurrency safety.
type StateStore interface {
	// Set associates the given value with the provided key for the
	// lifetime of the current request.
	//
	// Setting a value with an existing key replaces the previous value.
	Set(key string, val any)

	// Get retrieves the value associated with the given key.
	//
	// The returned boolean indicates whether the key was present.
	Get(key string) (any, bool)
}

// Context is the cross-framework surface passed into handlers and middleware.
//
// Context aggregates request inspection, request data binding, response
// writing, request-scoped state, and handler chain control into a single
// interface.
//
// Context is valid only for the lifetime of a single request and MUST NOT
// be retained or accessed after the request has completed.
//
// Implementations should provide consistent behavior across supported
// HTTP frameworks while respecting their underlying execution models.
type Context interface {
	// Request provides access to incoming request data.
	Request

	// Responder provides methods to write the outgoing response.
	Responder

	// Binder provides standardized request data decoding.
	Binder

	// StateStore provides request-scoped key-value storage.
	StateStore

	// Context provides access to the standard Go context.
	//
	// The returned context should be derived from the underlying
	// framework context and respect request cancellation and deadlines.
	context.Context

	// Next executes the next handler in the chain and returns any error
	// that occurred during execution.
	//
	// If an error is returned, the middleware chain should be interrupted
	// and the error should be handled appropriately.
	Next() error
}
