package httpx

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLimitRequestBody(t *testing.T) {
	t.Parallel()
	t.Run("DeclaredOverLimit", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("0123456789"))
		var mbe *http.MaxBytesError
		if err := LimitRequestBody(httptest.NewRecorder(), req, 4); !errors.As(err, &mbe) || mbe.Limit != 4 {
			t.Fatalf("err = %v, want *http.MaxBytesError{Limit: 4}", err)
		}
	})
	t.Run("UnknownLengthCutAtLimit", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/", io.NopCloser(strings.NewReader("0123456789")))
		req.ContentLength = -1
		if err := LimitRequestBody(httptest.NewRecorder(), req, 4); err != nil {
			t.Fatalf("err = %v", err)
		}
		var mbe *http.MaxBytesError
		if _, err := io.ReadAll(req.Body); !errors.As(err, &mbe) {
			t.Fatalf("read err = %v, want *http.MaxBytesError", err)
		}
	})
	t.Run("NoLimit", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("0123456789"))
		body := req.Body
		if err := LimitRequestBody(httptest.NewRecorder(), req, 0); err != nil || req.Body != body {
			t.Fatalf("err = %v, body replaced = %v; n <= 0 must leave the request alone", err, req.Body != body)
		}
	})
}
