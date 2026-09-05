package httpx

import (
	"errors"
	"net/http"
	"strings"
)

// StatusError represents an error that carries an HTTP status code.
// This interface allows errors to be categorized by their HTTP semantics.
type StatusError interface {
	error
	// GetStatus returns the HTTP status code associated with this error.
	GetStatus() int32
}

// CodeError represents an error that carries a custom error code.
// This is useful for application-specific error classification beyond HTTP status.
type CodeError interface {
	error
	// GetCode returns the custom error code associated with this error.
	GetCode() int32
}

// MessageError represents an error that carries a user-friendly message.
// This allows separation between technical error details and user-facing messages.
type MessageError interface {
	error
	// GetMessage returns the user-friendly message associated with this error.
	GetMessage() string
}

// Error is a comprehensive error type that includes HTTP status, custom code, and user message.
type Error interface {
	error
	StatusError
	CodeError
	MessageError
}

// httpError is an unexported concrete implementation of Error.
// Keeping it unexported prevents external code from depending on the
// concrete type, preserving flexibility to change internals.
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

// NewError creates a new error with HTTP status, custom code, and user message.
// It returns the broader Error interface to avoid exposing the concrete type
// and keep the API surface stable. If err is nil, a default HTTP error based on the
// status is used.
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

func WithStatus(status int32, err error, messages ...string) Error {
	code := int32(0)
	var se CodeError
	if errors.As(err, &se) {
		code = se.GetCode()
	}
	return NewError(status, code, strings.Join(messages, "; "), err)
}

func NewWithStatus(status int32, message string) Error {
	return NewError(status, 0, message, errors.New(message))
}

func BadRequestError(err error, messages ...string) Error {
	return WithStatus(http.StatusBadRequest, err, messages...)
}

func NewBadRequestError(message string) Error {
	return NewWithStatus(http.StatusBadRequest, message)
}

func UnauthorizedError(err error, messages ...string) Error {
	return WithStatus(http.StatusUnauthorized, err, messages...)
}

func NewUnauthorizedError(message string) Error {
	return NewWithStatus(http.StatusUnauthorized, message)
}

func ForbiddenError(err error, messages ...string) Error {
	return WithStatus(http.StatusForbidden, err, messages...)
}

func NewForbiddenError(message string) Error {
	return NewWithStatus(http.StatusForbidden, message)
}

func NotFoundError(err error, messages ...string) Error {
	return WithStatus(http.StatusNotFound, err, messages...)
}

func NewNotFoundError(message string) Error {
	return NewWithStatus(http.StatusNotFound, message)
}

func InternalServerError(err error, messages ...string) Error {
	return WithStatus(http.StatusInternalServerError, err, messages...)
}

func NewInternalServerError(message string) Error {
	return NewWithStatus(http.StatusInternalServerError, message)
}

// WrapBindError marks a binder failure as HTTP 400. Already classified
// httpx.Error values are returned unchanged. A nil err stays nil.
func WrapBindError(err error) error {
	if err == nil {
		return nil
	}
	var he Error
	if errors.As(err, &he) {
		return err
	}
	return BadRequestError(err)
}

// ErrorBody is the JSON written by adapter default error handlers.
// It matches the public fields of sphere/httpz.ErrorResponse (no debug Error).
type ErrorBody struct {
	Success bool   `json:"success"`
	Code    int    `json:"code"`
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

// ParseError extracts error information from various error types.
// It recognizes StatusError, CodeError, and MessageError interfaces and falls back
// to defaults for unknown error types.
// code is 0 unless err implements CodeError, matching ClassifyError.
func ParseError(err error) (code int32, status int32, message string) {
	var he Error
	if errors.As(err, &he) {
		return he.GetCode(), he.GetStatus(), he.GetMessage()
	}
	var se StatusError
	if errors.As(err, &se) {
		status = se.GetStatus()
	} else {
		status = http.StatusInternalServerError
	}
	var ce CodeError
	if errors.As(err, &ce) {
		code = ce.GetCode()
	}
	var me MessageError
	if errors.As(err, &me) {
		message = me.GetMessage()
	} else {
		message = err.Error()
	}
	return
}
