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
	Path() string
	FullPath() string // best-effort; empty if unsupported
	ClientIP() string

	Param(key string) string
	Params() map[string]string

	Query(key string) string
	Queries() map[string][]string
	RawQuery() string

	Header(key string) string
	Headers() map[string][]string

	Cookie(name string) (string, error)
	Cookies() map[string]string

	FormValue(key string) string
	MultipartForm() (*multipart.Form, error)
	FormFile(name string) (*multipart.FileHeader, error)

	BodyRaw() ([]byte, error)
	BodyReader() io.ReadCloser
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
	Next() error
}
