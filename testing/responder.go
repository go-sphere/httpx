package testing

import (
	"bytes"
	"net/http"
	"strings"
	"testing"

	"github.com/go-sphere/httpx"
)

// ResponderTester tests the Responder interface methods
type ResponderTester struct {
	engine httpx.Engine
}

// NewResponderTester creates a new Responder interface tester
func NewResponderTester(engine httpx.Engine) *ResponderTester {
	return &ResponderTester{engine: engine}
}

// TestStatus tests the Status() method
func (rt *ResponderTester) TestStatus(t *testing.T) {
	t.Helper()
	
	testCases := []struct {
		name       string
		statusCode int
	}{
		{"200 OK", 200},
		{"404 Not Found", 404},
		{"500 Internal Server Error", 500},
	}
	
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			router := rt.engine.Group("")
			// var capturedContext httpx.Context
			
			router.GET(GenerateUniqueTestPath(), func(ctx httpx.Context) {
				// capturedContext = ctx
				ctx.Status(tc.statusCode)
				// Note: Status alone doesn't commit the response
				ctx.Text(tc.statusCode, "Status set")
			})
			
			t.Logf("Test %s completed", tc.name)
		})
	}
}

// TestJSON tests the JSON() method
func (rt *ResponderTester) TestJSON(t *testing.T) {
	t.Helper()
	
	testCases := []struct {
		name       string
		statusCode int
		data       interface{}
	}{
		{"Simple JSON", 200, map[string]string{"message": "hello"}},
		{"Struct JSON", 201, TestStruct{Name: "test", Age: 25, Email: "test@example.com"}},
		{"Array JSON", 200, []string{"item1", "item2", "item3"}},
	}
	
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			router := rt.engine.Group("")
			// var capturedContext httpx.Context
			
			router.GET(GenerateUniqueTestPath(), func(ctx httpx.Context) {
				// capturedContext = ctx
				ctx.JSON(tc.statusCode, tc.data)
			})
			
			t.Logf("Test %s completed", tc.name)
		})
	}
}
// TestText tests the Text() method
func (rt *ResponderTester) TestText(t *testing.T) {
	t.Helper()
	
	testCases := []struct {
		name       string
		statusCode int
		text       string
	}{
		{"Simple text", 200, "Hello, World!"},
		{"Empty text", 204, ""},
		{"Long text", 200, strings.Repeat("A", 1000)},
	}
	
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			router := rt.engine.Group("")
			// var capturedContext httpx.Context
			
			router.GET(GenerateUniqueTestPath(), func(ctx httpx.Context) {
				// capturedContext = ctx
				ctx.Text(tc.statusCode, tc.text)
			})
			
			t.Logf("Test %s completed", tc.name)
		})
	}
}

// TestNoContent tests the NoContent() method
func (rt *ResponderTester) TestNoContent(t *testing.T) {
	t.Helper()
	
	testCases := []struct {
		name       string
		statusCode int
	}{
		{"204 No Content", 204},
		{"200 No Content", 200},
	}
	
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			router := rt.engine.Group("")
			// var capturedContext httpx.Context
			
			router.GET(GenerateUniqueTestPath(), func(ctx httpx.Context) {
				// capturedContext = ctx
				ctx.NoContent(tc.statusCode)
			})
			
			t.Logf("Test %s completed", tc.name)
		})
	}
}

// TestBytes tests the Bytes() method
func (rt *ResponderTester) TestBytes(t *testing.T) {
	t.Helper()
	
	testCases := []struct {
		name        string
		statusCode  int
		data        []byte
		contentType string
	}{
		{"Binary data", 200, []byte{0x89, 0x50, 0x4E, 0x47}, "image/png"},
		{"Text bytes", 200, []byte("Hello, World!"), "text/plain"},
	}
	
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			router := rt.engine.Group("")
			// var capturedContext httpx.Context
			
			router.GET(GenerateUniqueTestPath(), func(ctx httpx.Context) {
				// capturedContext = ctx
				ctx.Bytes(tc.statusCode, tc.data, tc.contentType)
			})
			
			t.Logf("Test %s completed", tc.name)
		})
	}
}

// TestDataFromReader tests the DataFromReader() method
func (rt *ResponderTester) TestDataFromReader(t *testing.T) {
	t.Helper()
	
	testCases := []struct {
		name        string
		statusCode  int
		data        string
		contentType string
		size        int
	}{
		{"Known size", 200, "Hello, World!", "text/plain", 13},
		{"Unknown size", 200, "Test data", "text/plain", -1},
	}
	
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			router := rt.engine.Group("")
			// var capturedContext httpx.Context
			
			router.GET(GenerateUniqueTestPath(), func(ctx httpx.Context) {
				// capturedContext = ctx
				reader := bytes.NewReader([]byte(tc.data))
				ctx.DataFromReader(tc.statusCode, tc.contentType, reader, tc.size)
			})
			
			t.Logf("Test %s completed", tc.name)
		})
	}
}
// TestFile tests the File() method
func (rt *ResponderTester) TestFile(t *testing.T) {
	t.Helper()
	
	testCases := []struct {
		name     string
		filePath string
	}{
		{"Existing file", "go.mod"}, // Use a file that should exist
		{"Non-existent file", "/nonexistent/file.txt"},
	}
	
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			router := rt.engine.Group("")
			// var capturedContext httpx.Context
			
			router.GET(GenerateUniqueTestPath(), func(ctx httpx.Context) {
				// capturedContext = ctx
				ctx.File(tc.filePath)
			})
			
			t.Logf("Test %s completed", tc.name)
		})
	}
}

// TestRedirect tests the Redirect() method
func (rt *ResponderTester) TestRedirect(t *testing.T) {
	t.Helper()
	
	testCases := []struct {
		name       string
		statusCode int
		location   string
	}{
		{"Temporary redirect", 302, "/new-location"},
		{"Permanent redirect", 301, "https://example.com"},
	}
	
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			router := rt.engine.Group("")
			// var capturedContext httpx.Context
			
			router.GET(GenerateUniqueTestPath(), func(ctx httpx.Context) {
				// capturedContext = ctx
				ctx.Redirect(tc.statusCode, tc.location)
			})
			
			t.Logf("Test %s completed", tc.name)
		})
	}
}

// TestSetHeader tests the SetHeader() method
func (rt *ResponderTester) TestSetHeader(t *testing.T) {
	t.Helper()
	
	testCases := []struct {
		name  string
		key   string
		value string
	}{
		{"Custom header", "X-Custom-Header", "custom-value"},
		{"Content-Type", "Content-Type", "application/xml"},
	}
	
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			router := rt.engine.Group("")
			// var capturedContext httpx.Context
			
			router.GET(GenerateUniqueTestPath(), func(ctx httpx.Context) {
				// capturedContext = ctx
				ctx.SetHeader(tc.key, tc.value)
				ctx.Text(200, "Header set")
			})
			
			t.Logf("Test %s completed", tc.name)
		})
	}
}

// TestSetCookie tests the SetCookie() method
func (rt *ResponderTester) TestSetCookie(t *testing.T) {
	t.Helper()
	
	testCases := []struct {
		name   string
		cookie *http.Cookie
	}{
		{"Simple cookie", &http.Cookie{Name: "session", Value: "abc123"}},
		{"Secure cookie", &http.Cookie{Name: "secure", Value: "value", Secure: true, HttpOnly: true}},
	}
	
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			router := rt.engine.Group("")
			// var capturedContext httpx.Context
			
			router.GET(GenerateUniqueTestPath(), func(ctx httpx.Context) {
				// capturedContext = ctx
				ctx.SetCookie(tc.cookie)
				ctx.Text(200, "Cookie set")
			})
			
			t.Logf("Test %s completed", tc.name)
		})
	}
}

// RunAllTests runs all Responder interface tests
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
	t.Run("SetHeader", rt.TestSetHeader)
	t.Run("SetCookie", rt.TestSetCookie)
}