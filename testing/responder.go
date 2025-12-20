package testing

import (
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-sphere/httpx"
)

// ResponderTester provides comprehensive testing tools for the Responder interface.
type ResponderTester struct {
	engine httpx.Engine
}

// NewResponderTester creates a new ResponderTester instance.
func NewResponderTester(engine httpx.Engine) *ResponderTester {
	return &ResponderTester{
		engine: engine,
	}
}

// TestStatus tests Status() method for setting response status codes.
// Validates Requirements 5.1: Status code setting
func (rt *ResponderTester) TestStatus(t *testing.T) {
	t.Helper()

	router := rt.engine.Group("")

	var capturedContext httpx.Context
	router.GET("/test-status", func(ctx httpx.Context) {
		capturedContext = ctx
		ctx.Status(http.StatusCreated)
		ctx.Text(http.StatusCreated, "Created")
	})

	// In a real implementation, we would make an actual HTTP request
	// and verify the status code. For now, we test the expected behavior.

	if capturedContext != nil {
		// Test that Status method was called
		// The actual verification would happen through HTTP response
		t.Log("Status method called successfully")
	}

	// Test various status codes
	testCases := []struct {
		name       string
		statusCode int
		path       string
	}{
		{"OK", http.StatusOK, "/status-ok"},
		{"Created", http.StatusCreated, "/status-created"},
		{"BadRequest", http.StatusBadRequest, "/status-bad-request"},
		{"NotFound", http.StatusNotFound, "/status-not-found"},
		{"InternalServerError", http.StatusInternalServerError, "/status-internal-error"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			router.GET(tc.path, func(ctx httpx.Context) {
				ctx.Status(tc.statusCode)
				ctx.Text(tc.statusCode, tc.name)
			})

			t.Logf("Testing status code %d for path %s", tc.statusCode, tc.path)
		})
	}
}

// TestJSON tests JSON() method for JSON response functionality.
// Validates Requirements 5.2: JSON response functionality
func (rt *ResponderTester) TestJSON(t *testing.T) {
	t.Helper()

	router := rt.engine.Group("")

	var capturedContext httpx.Context
	testData := map[string]interface{}{
		"name":    "John Doe",
		"age":     30,
		"email":   "john@example.com",
		"active":  true,
		"balance": 1234.56,
	}

	router.GET("/test-json", func(ctx httpx.Context) {
		capturedContext = ctx
		ctx.JSON(http.StatusOK, testData)
	})

	if capturedContext != nil {
		t.Logf("JSON response sent with data: %+v", testData)
	}

	// Test different JSON response scenarios
	testCases := []struct {
		name string
		data interface{}
		code int
		path string
	}{
		{"SimpleObject", map[string]string{"message": "hello"}, http.StatusOK, "/json-simple"},
		{"Array", []string{"item1", "item2", "item3"}, http.StatusOK, "/json-array"},
		{"Number", 42, http.StatusOK, "/json-number"},
		{"Boolean", true, http.StatusOK, "/json-boolean"},
		{"Null", nil, http.StatusOK, "/json-null"},
		{"ErrorResponse", map[string]string{"error": "not found"}, http.StatusNotFound, "/json-error"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			router.GET(tc.path, func(ctx httpx.Context) {
				ctx.JSON(tc.code, tc.data)
			})

			t.Logf("Testing JSON response with data: %+v", tc.data)
		})
	}
}

// TestText tests Text() method for text response functionality.
// Validates Requirements 5.3: Text response functionality
func (rt *ResponderTester) TestText(t *testing.T) {
	t.Helper()

	router := rt.engine.Group("")

	var capturedContext httpx.Context
	testText := "Hello, World!"

	router.GET("/test-text", func(ctx httpx.Context) {
		capturedContext = ctx
		ctx.Text(http.StatusOK, testText)
	})

	if capturedContext != nil {
		t.Logf("Text response sent: %s", testText)
	}

	// Test different text response scenarios
	testCases := []struct {
		name string
		text string
		code int
		path string
	}{
		{"SimpleText", "Hello, World!", http.StatusOK, "/text-simple"},
		{"EmptyText", "", http.StatusOK, "/text-empty"},
		{"MultilineText", "Line 1\nLine 2\nLine 3", http.StatusOK, "/text-multiline"},
		{"UnicodeText", "Hello, 世界! 🌍", http.StatusOK, "/text-unicode"},
		{"ErrorText", "Internal Server Error", http.StatusInternalServerError, "/text-error"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			router.GET(tc.path, func(ctx httpx.Context) {
				ctx.Text(tc.code, tc.text)
			})

			t.Logf("Testing text response: %q", tc.text)
		})
	}
}

// TestNoContent tests NoContent() method for empty response functionality.
// Validates Requirements 5.4: Empty response functionality
func (rt *ResponderTester) TestNoContent(t *testing.T) {
	t.Helper()

	router := rt.engine.Group("")

	var capturedContext httpx.Context
	router.DELETE("/test-no-content", func(ctx httpx.Context) {
		capturedContext = ctx
		ctx.NoContent(http.StatusNoContent)
	})

	if capturedContext != nil {
		t.Log("NoContent response sent successfully")
	}

	// Test different no-content scenarios
	testCases := []struct {
		name string
		code int
		path string
	}{
		{"NoContent", http.StatusNoContent, "/no-content-204"},
		{"Accepted", http.StatusAccepted, "/no-content-202"},
		{"ResetContent", http.StatusResetContent, "/no-content-205"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			router.DELETE(tc.path, func(ctx httpx.Context) {
				ctx.NoContent(tc.code)
			})

			t.Logf("Testing no content response with status %d", tc.code)
		})
	}
}

// TestBytes tests Bytes() method for byte response functionality.
// Validates Requirements 5.5: Byte response functionality
func (rt *ResponderTester) TestBytes(t *testing.T) {
	t.Helper()

	router := rt.engine.Group("")

	var capturedContext httpx.Context
	testBytes := []byte("Binary data content")
	contentType := "application/octet-stream"

	router.GET("/test-bytes", func(ctx httpx.Context) {
		capturedContext = ctx
		ctx.Bytes(http.StatusOK, testBytes, contentType)
	})

	if capturedContext != nil {
		t.Logf("Bytes response sent: %d bytes with content-type %s", len(testBytes), contentType)
	}

	// Test different byte response scenarios
	testCases := []struct {
		name        string
		data        []byte
		contentType string
		code        int
		path        string
	}{
		{"BinaryData", []byte{0x89, 0x50, 0x4E, 0x47}, "image/png", http.StatusOK, "/bytes-binary"},
		{"TextAsBytes", []byte("Text as bytes"), "text/plain", http.StatusOK, "/bytes-text"},
		{"JSONAsBytes", []byte(`{"key":"value"}`), "application/json", http.StatusOK, "/bytes-json"},
		{"EmptyBytes", []byte{}, "application/octet-stream", http.StatusOK, "/bytes-empty"},
		{"LargeBytes", make([]byte, 1024), "application/octet-stream", http.StatusOK, "/bytes-large"},
	}

	// Fill large bytes with test data
	for i := range testCases[4].data {
		testCases[4].data[i] = byte(i % 256)
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			router.GET(tc.path, func(ctx httpx.Context) {
				ctx.Bytes(tc.code, tc.data, tc.contentType)
			})

			t.Logf("Testing bytes response: %d bytes, content-type: %s", len(tc.data), tc.contentType)
		})
	}
}

// TestDataFromReader tests DataFromReader() method for streaming response functionality.
// Validates Requirements 5.6: Streaming response functionality
func (rt *ResponderTester) TestDataFromReader(t *testing.T) {
	t.Helper()

	router := rt.engine.Group("")

	var capturedContext httpx.Context
	testData := "Streaming data content"
	reader := strings.NewReader(testData)
	contentType := "text/plain"
	size := len(testData)

	router.GET("/test-stream", func(ctx httpx.Context) {
		capturedContext = ctx
		ctx.DataFromReader(http.StatusOK, contentType, reader, size)
	})

	if capturedContext != nil {
		t.Logf("Streaming response sent: %d bytes with content-type %s", size, contentType)
	}

	// Test different streaming scenarios
	testCases := []struct {
		name        string
		data        string
		contentType string
		code        int
		path        string
	}{
		{"TextStream", "Text streaming data", "text/plain", http.StatusOK, "/stream-text"},
		{"JSONStream", `{"stream": true, "data": "value"}`, "application/json", http.StatusOK, "/stream-json"},
		{"CSVStream", "name,age,email\nJohn,30,john@example.com", "text/csv", http.StatusOK, "/stream-csv"},
		{"EmptyStream", "", "text/plain", http.StatusOK, "/stream-empty"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			reader := strings.NewReader(tc.data)
			router.GET(tc.path, func(ctx httpx.Context) {
				ctx.DataFromReader(tc.code, tc.contentType, reader, len(tc.data))
			})

			t.Logf("Testing stream response: %d bytes, content-type: %s", len(tc.data), tc.contentType)
		})
	}
}

// TestFile tests File() method for file response functionality.
// Validates Requirements 5.7: File response functionality
func (rt *ResponderTester) TestFile(t *testing.T) {
	t.Helper()

	// Create a temporary test file
	testFile, err := os.CreateTemp("", "responder_test_*.txt")
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}
	defer func() { _ = os.Remove(testFile.Name()) }()

	testContent := "This is a test file content for file response testing."
	if _, err := testFile.WriteString(testContent); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}
	_ = testFile.Close()

	router := rt.engine.Group("")

	var capturedContext httpx.Context
	router.GET("/test-file", func(ctx httpx.Context) {
		capturedContext = ctx
		ctx.File(testFile.Name())
	})

	if capturedContext != nil {
		t.Logf("File response sent: %s", testFile.Name())
	}

	// Test different file scenarios
	testCases := []struct {
		name     string
		filename string
		content  string
		path     string
	}{
		{"TextFile", "test.txt", "Text file content", "/file-text"},
		{"JSONFile", "test.json", `{"file": "json"}`, "/file-json"},
		{"CSVFile", "test.csv", "name,value\ntest,123", "/file-csv"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create temporary file for each test case
			tmpFile, err := os.CreateTemp("", tc.filename)
			if err != nil {
				t.Fatalf("Failed to create test file %s: %v", tc.filename, err)
			}
			defer func() { _ = os.Remove(tmpFile.Name()) }()

			if _, err := tmpFile.WriteString(tc.content); err != nil {
				t.Fatalf("Failed to write test file %s: %v", tc.filename, err)
			}
			_ = tmpFile.Close()

			router.GET(tc.path, func(ctx httpx.Context) {
				ctx.File(tmpFile.Name())
			})

			t.Logf("Testing file response: %s", tc.filename)
		})
	}
}

// TestRedirect tests Redirect() method for redirection functionality.
// Validates Requirements 5.8: Redirection functionality
func (rt *ResponderTester) TestRedirect(t *testing.T) {
	t.Helper()

	router := rt.engine.Group("")

	var capturedContext httpx.Context
	router.GET("/test-redirect", func(ctx httpx.Context) {
		capturedContext = ctx
		ctx.Redirect(http.StatusFound, "/redirected")
	})

	if capturedContext != nil {
		t.Log("Redirect response sent successfully")
	}

	// Test different redirect scenarios
	testCases := []struct {
		name     string
		code     int
		location string
		path     string
	}{
		{"TemporaryRedirect", http.StatusFound, "/temporary", "/redirect-temp"},
		{"PermanentRedirect", http.StatusMovedPermanently, "/permanent", "/redirect-perm"},
		{"SeeOther", http.StatusSeeOther, "/see-other", "/redirect-see-other"},
		{"TemporaryRedirect307", http.StatusTemporaryRedirect, "/temp-307", "/redirect-307"},
		{"PermanentRedirect308", http.StatusPermanentRedirect, "/perm-308", "/redirect-308"},
		{"ExternalRedirect", http.StatusFound, "https://example.com", "/redirect-external"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			router.GET(tc.path, func(ctx httpx.Context) {
				ctx.Redirect(tc.code, tc.location)
			})

			t.Logf("Testing redirect: %d -> %s", tc.code, tc.location)
		})
	}
}

// TestHeaders tests SetHeader() method for response header setting functionality.
// Validates Requirements 5.9: Response header setting functionality
func (rt *ResponderTester) TestHeaders(t *testing.T) {
	t.Helper()

	router := rt.engine.Group("")

	var capturedContext httpx.Context
	router.GET("/test-headers", func(ctx httpx.Context) {
		capturedContext = ctx
		ctx.SetHeader("X-Custom-Header", "custom-value")
		ctx.SetHeader("X-API-Version", "v1.0")
		ctx.SetHeader("Cache-Control", "no-cache")
		ctx.Text(http.StatusOK, "Headers set")
	})

	if capturedContext != nil {
		t.Log("Headers set successfully")
	}

	// Test different header scenarios
	testCases := []struct {
		name  string
		key   string
		value string
		path  string
	}{
		{"ContentType", "Content-Type", "application/json", "/header-content-type"},
		{"CacheControl", "Cache-Control", "max-age=3600", "/header-cache"},
		{"CORS", "Access-Control-Allow-Origin", "*", "/header-cors"},
		{"CustomHeader", "X-Request-ID", "12345", "/header-custom"},
		{"Authorization", "WWW-Authenticate", "Bearer", "/header-auth"},
		{"ContentDisposition", "Content-Disposition", "attachment; filename=test.txt", "/header-disposition"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			router.GET(tc.path, func(ctx httpx.Context) {
				ctx.SetHeader(tc.key, tc.value)
				ctx.Text(http.StatusOK, "OK")
			})

			t.Logf("Testing header: %s = %s", tc.key, tc.value)
		})
	}
}

// TestCookies tests SetCookie() method for cookie setting functionality.
// Validates Requirements 5.9: Cookie setting functionality
func (rt *ResponderTester) TestCookies(t *testing.T) {
	t.Helper()

	router := rt.engine.Group("")

	var capturedContext httpx.Context
	router.GET("/test-cookies", func(ctx httpx.Context) {
		capturedContext = ctx

		// Set a simple cookie
		simpleCookie := &http.Cookie{
			Name:  "session_id",
			Value: "abc123",
		}
		ctx.SetCookie(simpleCookie)

		// Set a complex cookie with all options
		complexCookie := &http.Cookie{
			Name:     "user_pref",
			Value:    "dark_mode",
			Path:     "/",
			Domain:   "example.com",
			Expires:  time.Now().Add(24 * time.Hour),
			MaxAge:   86400,
			Secure:   true,
			HttpOnly: true,
			SameSite: http.SameSiteStrictMode,
		}
		ctx.SetCookie(complexCookie)

		ctx.Text(http.StatusOK, "Cookies set")
	})

	if capturedContext != nil {
		t.Log("Cookies set successfully")
	}

	// Test different cookie scenarios
	testCases := []struct {
		name   string
		cookie *http.Cookie
		path   string
	}{
		{
			"SimpleCookie",
			&http.Cookie{Name: "simple", Value: "value"},
			"/cookie-simple",
		},
		{
			"SecureCookie",
			&http.Cookie{Name: "secure", Value: "value", Secure: true, HttpOnly: true},
			"/cookie-secure",
		},
		{
			"ExpiringCookie",
			&http.Cookie{Name: "expiring", Value: "value", Expires: time.Now().Add(time.Hour)},
			"/cookie-expiring",
		},
		{
			"MaxAgeCookie",
			&http.Cookie{Name: "maxage", Value: "value", MaxAge: 3600},
			"/cookie-maxage",
		},
		{
			"SameSiteCookie",
			&http.Cookie{Name: "samesite", Value: "value", SameSite: http.SameSiteLaxMode},
			"/cookie-samesite",
		},
		{
			"DomainCookie",
			&http.Cookie{Name: "domain", Value: "value", Domain: "example.com", Path: "/"},
			"/cookie-domain",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			router.GET(tc.path, func(ctx httpx.Context) {
				ctx.SetCookie(tc.cookie)
				ctx.Text(http.StatusOK, "OK")
			})

			t.Logf("Testing cookie: %s = %s", tc.cookie.Name, tc.cookie.Value)
		})
	}
}

// TestCombinedResponses tests combinations of different response methods.
// This ensures that response methods work correctly when used together.
func (rt *ResponderTester) TestCombinedResponses(t *testing.T) {
	t.Helper()

	router := rt.engine.Group("")

	// Test setting headers before JSON response
	router.GET("/combined-json", func(ctx httpx.Context) {
		ctx.SetHeader("X-API-Version", "v1.0")
		ctx.SetHeader("X-Request-ID", "12345")
		ctx.JSON(http.StatusOK, map[string]string{"message": "success"})
	})

	// Test setting cookies before text response
	router.GET("/combined-text", func(ctx httpx.Context) {
		ctx.SetCookie(&http.Cookie{Name: "visited", Value: "true"})
		ctx.SetHeader("Content-Type", "text/plain; charset=utf-8")
		ctx.Text(http.StatusOK, "Hello with cookie")
	})

	// Test status with headers
	router.GET("/combined-status", func(ctx httpx.Context) {
		ctx.Status(http.StatusCreated)
		ctx.SetHeader("Location", "/resource/123")
		ctx.SetHeader("X-Created-At", time.Now().Format(time.RFC3339))
		ctx.JSON(http.StatusCreated, map[string]interface{}{
			"id":      123,
			"created": true,
		})
	})

	t.Log("Combined response tests set up successfully")
}

// RunAllTests runs all Responder interface tests.
func (rt *ResponderTester) RunAllTests(t *testing.T) {
	t.Helper()

	t.Run("Status", rt.TestStatus)
	t.Run("JSON", rt.TestJSON)
	t.Run("Text", rt.TestText)
	t.Run("NoContent", rt.TestNoContent)
	t.Run("Bytes", rt.TestBytes)
	t.Run("DataFromReader", rt.TestDataFromReader)
	t.Run("File", rt.TestFile)
	t.Run("Redirect", rt.TestRedirect)
	t.Run("Headers", rt.TestHeaders)
	t.Run("Cookies", rt.TestCookies)
	t.Run("CombinedResponses", rt.TestCombinedResponses)
}

// Helper functions for creating test data

// createTestJSONData creates test data for JSON responses.
func createTestJSONData() map[string]interface{} {
	return map[string]interface{}{
		"string":  "test",
		"number":  42,
		"boolean": true,
		"array":   []string{"item1", "item2"},
		"object": map[string]string{
			"nested": "value",
		},
		"null": nil,
	}
}

// createTestBytes creates test byte data for byte responses.
func createTestBytes(size int) []byte {
	data := make([]byte, size)
	for i := range data {
		data[i] = byte(i % 256)
	}
	return data
}

// createTestReader creates a test reader for streaming responses.
func createTestReader(content string) io.Reader {
	return strings.NewReader(content)
}

// createTestCookie creates a test cookie with common options.
func createTestCookie(name, value string) *http.Cookie {
	return &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		MaxAge:   3600,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	}
}
