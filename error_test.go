package httpx

import (
	"errors"
	"net/http"
	"strings"
	"testing"
)

func TestNewXxxErrorSetsGetMessage(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		err     Error
		status  int32
		message string
	}{
		{"bad request", NewBadRequestError("missing id"), http.StatusBadRequest, "missing id"},
		{"unauthorized", NewUnauthorizedError("login required"), http.StatusUnauthorized, "login required"},
		{"forbidden", NewForbiddenError("no permission to access this resource"), http.StatusForbidden, "no permission to access this resource"},
		{"not found", NewNotFoundError("filename is required"), http.StatusNotFound, "filename is required"},
		{"internal", NewInternalServerError("unavailable"), http.StatusInternalServerError, "unavailable"},
		{"with status", NewWithStatus(http.StatusTooManyRequests, "rate limit exceeded"), http.StatusTooManyRequests, "rate limit exceeded"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.err.GetStatus() != tc.status {
				t.Fatalf("status = %d, want %d", tc.err.GetStatus(), tc.status)
			}
			if tc.err.GetMessage() != tc.message {
				t.Fatalf("message = %q, want %q", tc.err.GetMessage(), tc.message)
			}
			if tc.err.GetCode() != 0 {
				t.Fatalf("code = %d, want 0", tc.err.GetCode())
			}
		})
	}
}

func TestWrapBindError(t *testing.T) {
	t.Parallel()
	if got := WrapBindError(nil); got != nil {
		t.Fatalf("nil: %v", got)
	}
	classified := NewBadRequestError("already")
	if got := WrapBindError(classified); got != classified {
		t.Fatalf("classified: got %#v", got)
	}
	raw := errors.New("invalid character")
	got := WrapBindError(raw)
	var he Error
	if !errors.As(got, &he) {
		t.Fatalf("expected httpx.Error, got %T", got)
	}
	if he.GetStatus() != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", he.GetStatus())
	}
}

func TestClassifyError(t *testing.T) {
	t.Parallel()
	code, status, message := ClassifyError(errors.New("pq: password authentication failed"))
	if code != 0 || status != http.StatusInternalServerError || message != http.StatusText(http.StatusInternalServerError) {
		t.Fatalf("unclassified: code=%d status=%d message=%q", code, status, message)
	}

	code, status, message = ClassifyError(NewForbiddenError("no permission to access this resource"))
	if code != 0 || status != http.StatusForbidden || message != "no permission to access this resource" {
		t.Fatalf("forbidden: code=%d status=%d message=%q", code, status, message)
	}

	_, status, message = ClassifyError(UnauthorizedError(errors.New("token is expired")))
	if status != http.StatusUnauthorized || message != http.StatusText(http.StatusUnauthorized) {
		t.Fatalf("empty message: status=%d message=%q", status, message)
	}
}

// ParseError is the default ErrorParser in sphere/httpz, so nil must classify
// like any other error carrying no information — 500, no message — rather than
// panic. ClassifyError substitutes the status text, hence the deliberate gap.
func TestParseErrorNil(t *testing.T) {
	t.Parallel()
	code, status, message := ParseError(nil)
	if code != 0 || status != http.StatusInternalServerError || message != "" {
		t.Fatalf("ParseError(nil) = (%d, %d, %q), want (0, 500, \"\")", code, status, message)
	}

	unclassifiedCode, unclassifiedStatus, unclassifiedMessage := ParseError(errors.New("boom"))
	if code != unclassifiedCode || status != unclassifiedStatus || message != unclassifiedMessage {
		t.Fatalf("ParseError(nil) = (%d, %d, %q), want the unclassified triple (%d, %d, %q)",
			code, status, message, unclassifiedCode, unclassifiedStatus, unclassifiedMessage)
	}

	classifiedCode, classifiedStatus, classifiedMessage := ClassifyError(nil)
	if classifiedCode != 0 || classifiedStatus != http.StatusInternalServerError ||
		classifiedMessage != http.StatusText(http.StatusInternalServerError) {
		t.Fatalf("ClassifyError(nil) = (%d, %d, %q), want (0, 500, %q)",
			classifiedCode, classifiedStatus, classifiedMessage, http.StatusText(http.StatusInternalServerError))
	}
}

// The three sub-interfaces, implemented separately, so ParseError is exercised
// on the partial shapes an application error can have — not just httpx.Error.
type statusOnlyError struct{ error }

func (statusOnlyError) GetStatus() int32 { return http.StatusTeapot }

type codeOnlyError struct{ error }

func (codeOnlyError) GetCode() int32 { return 4041 }

type messageOnlyError struct {
	error
	message string
}

func (e messageOnlyError) GetMessage() string { return e.message }

// ParseError feeds an HTTP response body through sphere/httpz, so an error with
// no MessageError must yield no message rather than err.Error(). ClassifyError
// is the rendering path and substitutes the status text; its output — which
// adapter error bodies and the golden files depend on — must not move.
func TestParseErrorDoesNotLeakErrorString(t *testing.T) {
	t.Parallel()
	const raw = "pq: password authentication failed for user \"admin\""
	cases := []struct {
		name string
		err  error
		// what ParseError must report
		code    int32
		status  int32
		message string
		// what ClassifyError must still report
		classifiedCode    int32
		classifiedStatus  int32
		classifiedMessage string
	}{
		{
			name: "unclassified", err: errors.New(raw),
			code: 0, status: http.StatusInternalServerError, message: "",
			classifiedCode: 0, classifiedStatus: http.StatusInternalServerError, classifiedMessage: http.StatusText(http.StatusInternalServerError),
		},
		{
			name: "status only", err: statusOnlyError{errors.New(raw)},
			code: 0, status: http.StatusTeapot, message: "",
			classifiedCode: 0, classifiedStatus: http.StatusTeapot, classifiedMessage: http.StatusText(http.StatusTeapot),
		},
		{
			name: "code only", err: codeOnlyError{errors.New(raw)},
			code: 4041, status: http.StatusInternalServerError, message: "",
			classifiedCode: 4041, classifiedStatus: http.StatusInternalServerError, classifiedMessage: http.StatusText(http.StatusInternalServerError),
		},
		{
			// An explicit but empty message is still "no message": it must not
			// fall back to err.Error() either.
			name: "message only, empty", err: messageOnlyError{errors.New(raw), ""},
			code: 0, status: http.StatusInternalServerError, message: "",
			classifiedCode: 0, classifiedStatus: http.StatusInternalServerError, classifiedMessage: http.StatusText(http.StatusInternalServerError),
		},
		{
			name: "message only, real", err: messageOnlyError{errors.New(raw), "please try again later"},
			code: 0, status: http.StatusInternalServerError, message: "please try again later",
			classifiedCode: 0, classifiedStatus: http.StatusInternalServerError, classifiedMessage: "please try again later",
		},
		{
			name: "full httpx.Error", err: NewError(http.StatusForbidden, 4030, "no permission", errors.New(raw)),
			code: 4030, status: http.StatusForbidden, message: "no permission",
			classifiedCode: 4030, classifiedStatus: http.StatusForbidden, classifiedMessage: "no permission",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, status, message := ParseError(tc.err)
			if code != tc.code || status != tc.status || message != tc.message {
				t.Fatalf("ParseError = (%d, %d, %q), want (%d, %d, %q)", code, status, message, tc.code, tc.status, tc.message)
			}
			if message == tc.err.Error() && tc.err.Error() != tc.message {
				t.Fatalf("ParseError leaked err.Error(): %q", message)
			}

			code, status, message = ClassifyError(tc.err)
			if code != tc.classifiedCode || status != tc.classifiedStatus || message != tc.classifiedMessage {
				t.Fatalf("ClassifyError = (%d, %d, %q), want (%d, %d, %q)", code, status, message, tc.classifiedCode, tc.classifiedStatus, tc.classifiedMessage)
			}

			renderStatus, body := RenderError(tc.err)
			if renderStatus != int(tc.classifiedStatus) || body.Success || body.Code != int(tc.classifiedCode) || body.Message != tc.classifiedMessage {
				t.Fatalf("RenderError = (%d, %+v), want (%d, {false %d %q})", renderStatus, body, tc.classifiedStatus, tc.classifiedCode, tc.classifiedMessage)
			}
			if strings.Contains(body.Message, "password authentication failed") {
				t.Fatalf("RenderError leaked the raw error: %q", body.Message)
			}
		})
	}
}

func TestRenderErrorDoesNotLeak(t *testing.T) {
	t.Parallel()
	const raw = "pq: password authentication failed for user \"admin\""
	status, body := RenderError(errors.New(raw))
	if status != http.StatusInternalServerError {
		t.Fatalf("status = %d", status)
	}
	if body.Success {
		t.Fatal("expected success=false")
	}
	if body.Message == raw || body.Message != http.StatusText(http.StatusInternalServerError) {
		t.Fatalf("leaked or wrong message: %q", body.Message)
	}
}
