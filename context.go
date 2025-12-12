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

	Param(key string) string
	Params() map[string]string

	Query(key string) string
	Queries() map[string][]string

	FormValue(key string) string
	FormValues() map[string][]string
	FormFile(name string) (*multipart.FileHeader, error)

	Header(key string) string
	Cookie(name string) (string, error)
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

	JSON(code int, v any) error
	Text(code int, s string) error
	Bytes(code int, b []byte, contentType string) error
	Stream(code int, contentType string, fn func(w io.Writer) error) error
	File(path string) error
	Redirect(code int, location string) error

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
	AbortWithStatus(code int)
	AbortWithError(code int, err error)
	IsAborted() bool
}

// Context is the cross-framework surface passed into handlers.
type Context interface {
	context.Context
	Request
	Responder
	Binder
	StateStore
	Aborter
}
