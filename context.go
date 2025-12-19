package httpx

import (
	"context"
	"io"
	"mime/multipart"
	"net/http"
)

type H map[string]any

// Request exposes a common, read-only view over incoming HTTP requests.
type Request interface {
	Method() string
	Path() string     // Always returns request path
	FullPath() string // Returns route pattern when available, empty otherwise
	ClientIP() string // Best-effort client IP detection

	// Parameter access with consistent behavior
	Param(key string) string
	Params() map[string]string // nil if no params

	// Query handling with normalized behavior
	Query(key string) string
	Queries() map[string][]string // nil if no queries
	RawQuery() string

	// Header access with canonical keys
	Header(key string) string
	Headers() map[string][]string // Canonical MIME header keys

	// Cookie handling with consistent error behavior
	Cookie(name string) (string, error) // Returns http.ErrNoCookie when not found
	Cookies() map[string]string         // nil if no cookies

	// Form data access
	FormValue(key string) string
	MultipartForm() (*multipart.Form, error)
	FormFile(name string) (*multipart.FileHeader, error)

	// Body access with clear consumption semantics
	BodyRaw() ([]byte, error)  // May consume body
	BodyReader() io.ReadCloser // Returns reader, may be consumed
}

// Binder standardizes payload decoding across frameworks.
type Binder interface {
	BindJSON(dst any) error
	BindQuery(dst any) error
	BindForm(dst any) error
	BindURI(dst any) error
	BindHeader(dst any) error
}

// Responder writes responses across frameworks.
type Responder interface {
	Status(code int)

	JSON(code int, v any)
	Text(code int, s string)
	NoContent(code int)
	Bytes(code int, b []byte, contentType string)
	DataFromReader(code int, contentType string, r io.Reader, size int)
	File(path string)
	Redirect(code int, location string)

	SetHeader(key, value string)
	SetCookie(cookie *http.Cookie)
}

// StateStore carries request-scoped values.
type StateStore interface {
	Set(key string, val any)
	Get(key string) (any, bool)
}

// Aborter allows a handler to short-circuit the remaining chain.
type Aborter interface {
	Abort()
	IsAborted() bool
}

// Context is the cross-framework surface passed into handlers.
type Context interface {
	Request
	Responder
	Binder
	StateStore
	Aborter
	context.Context
	Next()
}
