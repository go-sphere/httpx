package testing

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-sphere/httpx"
)

// EqualSlices compares two slices of comparable types for equality.
// Returns true if both slices have the same length and all elements are equal.
func EqualSlices[T comparable](a, b []T) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// CreateTestServer creates an httptest.Server from an httpx.Engine for testing purposes.
func CreateTestServer(engine httpx.Engine) *httptest.Server {
	// This is a placeholder - actual implementation will depend on how engines expose their http.Handler
	// For now, we'll return nil and implement this when we have concrete engine implementations
	return nil
}

// MakeRequest creates an HTTP request with the specified parameters.
func MakeRequest(method, url string, body io.Reader, headers map[string]string) (*http.Request, error) {
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, err
	}

	for key, value := range headers {
		req.Header.Set(key, value)
	}

	return req, nil
}

// AssertResponse verifies that an HTTP response matches expected values.
func AssertResponse(t *testing.T, resp *http.Response, expectedCode int, expectedBody string) {
	t.Helper()

	if resp == nil {
		t.Error("Response is nil")
		return
	}

	if resp.StatusCode != expectedCode {
		t.Errorf("Expected status code %d, got %d", expectedCode, resp.StatusCode)
	}

	if expectedBody != "" {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Errorf("Failed to read response body: %v", err)
			return
		}
		defer func() { _ = resp.Body.Close() }()

		if string(body) != expectedBody {
			t.Errorf("Expected body %q, got %q", expectedBody, string(body))
		}
	}
}

// AssertHeader verifies that an HTTP response contains the expected header value.
func AssertHeader(t *testing.T, resp *http.Response, key, expectedValue string) {
	t.Helper()

	if resp == nil {
		t.Error("Response is nil")
		return
	}

	actualValue := resp.Header.Get(key)
	if actualValue != expectedValue {
		t.Errorf("Expected header %s to be %q, got %q", key, expectedValue, actualValue)
	}
}

// AssertCookie verifies that an HTTP response contains the expected cookie value.
func AssertCookie(t *testing.T, resp *http.Response, name, expectedValue string) {
	t.Helper()

	cookies := resp.Cookies()
	for _, cookie := range cookies {
		if cookie.Name == name {
			if cookie.Value != expectedValue {
				t.Errorf("Expected cookie %s to be %q, got %q", name, expectedValue, cookie.Value)
			}
			return
		}
	}

	t.Errorf("Cookie %s not found in response", name)
}
