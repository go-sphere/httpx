package httpx

import (
	"errors"
	"net/http"
	"strings"
)

// StatusError is an error carrying an HTTP status code.
type StatusError interface {
	error
	// GetStatus returns the HTTP status code the error should be rendered with.
	GetStatus() int32
}

// CodeError is an error carrying an application-specific error code.
type CodeError interface {
	error
	// GetCode returns the application-specific error code.
	GetCode() int32
}

// MessageError is an error carrying a user-facing message, separate from the
// technical detail in Error().
type MessageError interface {
	error
	// GetMessage returns the message that is safe to show to clients; it
	// may be empty.
	GetMessage() string
}

// Error carries an HTTP status, an application code and a user message.
// Construct one with NewError, WithStatus or a status helper such as
// NewNotFoundError or BadRequestError; the concrete type is unexported. The
// returned values unwrap to the error they were built from.
type Error interface {
	error
	StatusError
	CodeError
	MessageError
}

// httpError implements Error. It stays unexported so callers depend on the
// interface rather than the concrete type.
type httpError struct {
	error
	status  int32
	code    int32
	message string
}

func (e *httpError) GetStatus() int32 {
	return e.status
}

func (e *httpError) GetCode() int32 {
	return e.code
}

func (e *httpError) GetMessage() string {
	return e.message
}

func (e *httpError) Error() string {
	return e.error.Error()
}

func (e *httpError) Unwrap() error {
	return e.error
}

// NewError creates an error with an HTTP status, an application code and a user
// message. If err is nil, a default error derived from the status is used.
func NewError(status, code int32, message string, err error) Error {
	if err == nil {
		err = httpStatusError(status)
	}
	return &httpError{
		error:   err,
		status:  status,
		code:    code,
		message: message,
	}
}

// WithStatus wraps err as an Error with the given HTTP status. The code is
// taken from the first CodeError in err's chain, or 0. The user message is
// messages joined with "; ", and empty when none are given; err's own text
// is never used as the message. The new status takes precedence over any
// status err already carries. A nil err is replaced by an error whose text is
// the status text.
func WithStatus(status int32, err error, messages ...string) Error {
	code := int32(0)
	var se CodeError
	if errors.As(err, &se) {
		code = se.GetCode()
	}
	return NewError(status, code, strings.Join(messages, "; "), err)
}

// NewWithStatus returns a new Error with the given HTTP status and code 0,
// using message both as the user message and as the Error() text.
func NewWithStatus(status int32, message string) Error {
	return NewError(status, 0, message, errors.New(message))
}

// BadRequestError wraps err with status 400; see WithStatus.
func BadRequestError(err error, messages ...string) Error {
	return WithStatus(http.StatusBadRequest, err, messages...)
}

// NewBadRequestError returns a new 400 Error carrying message; see
// NewWithStatus.
func NewBadRequestError(message string) Error {
	return NewWithStatus(http.StatusBadRequest, message)
}

// UnauthorizedError wraps err with status 401; see WithStatus.
func UnauthorizedError(err error, messages ...string) Error {
	return WithStatus(http.StatusUnauthorized, err, messages...)
}

// NewUnauthorizedError returns a new 401 Error carrying message; see
// NewWithStatus.
func NewUnauthorizedError(message string) Error {
	return NewWithStatus(http.StatusUnauthorized, message)
}

// ForbiddenError wraps err with status 403; see WithStatus.
func ForbiddenError(err error, messages ...string) Error {
	return WithStatus(http.StatusForbidden, err, messages...)
}

// NewForbiddenError returns a new 403 Error carrying message; see
// NewWithStatus.
func NewForbiddenError(message string) Error {
	return NewWithStatus(http.StatusForbidden, message)
}

// NotFoundError wraps err with status 404; see WithStatus.
func NotFoundError(err error, messages ...string) Error {
	return WithStatus(http.StatusNotFound, err, messages...)
}

// NewNotFoundError returns a new 404 Error carrying message; see
// NewWithStatus.
func NewNotFoundError(message string) Error {
	return NewWithStatus(http.StatusNotFound, message)
}

// InternalServerError wraps err with status 500; see WithStatus.
func InternalServerError(err error, messages ...string) Error {
	return WithStatus(http.StatusInternalServerError, err, messages...)
}

// NewInternalServerError returns a new 500 Error carrying message; see
// NewWithStatus.
func NewInternalServerError(message string) Error {
	return NewWithStatus(http.StatusInternalServerError, message)
}

// WrapBindError marks a binder failure as HTTP 400, or as 413 when err's chain
// holds a *http.MaxBytesError (the request body exceeded its limit). Already
// classified httpx.Error values are returned unchanged. A nil err stays nil.
func WrapBindError(err error) error {
	if err == nil {
		return nil
	}
	var he Error
	if errors.As(err, &he) {
		return err
	}
	if isBodyTooLarge(err) {
		return WithStatus(http.StatusRequestEntityTooLarge, err)
	}
	return BadRequestError(err)
}

func isBodyTooLarge(err error) bool {
	var mbe *http.MaxBytesError
	return errors.As(err, &mbe)
}

// ErrorBody is the JSON written by adapter default error handlers.
// It matches the public fields of sphere/httpz.ErrorResponse (no debug Error).
type ErrorBody struct {
	// Success is always false for an error body.
	Success bool `json:"success"`
	// Code is the application code from CodeError, or 0.
	Code int `json:"code"`
	// Message is the error's user message, or the HTTP status text when it
	// carries none.
	Message string `json:"message"`
}

// RenderError maps err to an HTTP status and a non-leaking JSON body.
// Unclassified errors report code 0 and the generic status text.
func RenderError(err error) (status int, body ErrorBody) {
	code, status32, message := ClassifyError(err)
	return int(status32), ErrorBody{
		Success: false,
		Code:    int(code),
		Message: message,
	}
}

// ClassifyError extracts application code, HTTP status, and a user-facing
// message. Status is clamped to 100–599. code is 0 unless err implements
// CodeError. message is the generic status text unless err implements
// MessageError with a non-empty message.
func ClassifyError(err error) (code int32, status int32, message string) {
	if err == nil {
		return 0, http.StatusInternalServerError, http.StatusText(http.StatusInternalServerError)
	}
	code, status, message = ParseError(err)
	if status < 100 || status > 599 {
		status = http.StatusInternalServerError
	}
	var ce CodeError
	if !errors.As(err, &ce) {
		code = 0
	}
	var me MessageError
	if !errors.As(err, &me) || message == "" {
		message = http.StatusText(int(status))
	}
	return
}

func httpStatusError(status int32) error {
	msg := http.StatusText(int(status))
	if msg == "" {
		msg = "Unknown error"
	}
	return errors.New(msg)
}

// ParseError extracts status, code and message from StatusError, CodeError and
// MessageError, falling back to defaults for unknown error types: status 413
// when err's chain holds a *http.MaxBytesError, 500 otherwise.
//
// message is empty unless err carries one through MessageError, and is never
// err.Error(): ParseError is the default ErrorParser of sphere/httpz, so its
// message goes straight into a response body, where err.Error() would leak raw
// internal detail — driver strings, SQL, panic text. Callers that need
// something to render should use ClassifyError or RenderError, which substitute
// http.StatusText(status).
func ParseError(err error) (code int32, status int32, message string) {
	// A nil error is a caller mistake that must not take the process down; it
	// classifies like any other error carrying no information.
	if err == nil {
		return 0, http.StatusInternalServerError, ""
	}
	var he Error
	if errors.As(err, &he) {
		return he.GetCode(), he.GetStatus(), he.GetMessage()
	}
	var se StatusError
	switch {
	case errors.As(err, &se):
		status = se.GetStatus()
	case isBodyTooLarge(err):
		status = http.StatusRequestEntityTooLarge
	default:
		status = http.StatusInternalServerError
	}
	var ce CodeError
	if errors.As(err, &ce) {
		code = ce.GetCode()
	}
	var me MessageError
	if errors.As(err, &me) {
		message = me.GetMessage()
	}
	return
}
