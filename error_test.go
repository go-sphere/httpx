package httpx

import (
	"errors"
	"net/http"
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

	code, status, message = ClassifyError(UnauthorizedError(errors.New("token is expired")))
	if status != http.StatusUnauthorized || message != http.StatusText(http.StatusUnauthorized) {
		t.Fatalf("empty message: status=%d message=%q", status, message)
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
