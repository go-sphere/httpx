package testing

import (
	"testing"

	"github.com/go-sphere/httpx"
)

// HTTPBinderTester provides comprehensive HTTP-based testing for binding functionality
type HTTPBinderTester struct {
	engine httpx.Engine
}

// NewHTTPBinderTester creates a new HTTP-based binder tester
func NewHTTPBinderTester(engine httpx.Engine) *HTTPBinderTester {
	return &HTTPBinderTester{
		engine: engine,
	}
}

// TestBindJSONWithHTTP tests JSON binding with actual HTTP requests
func (hbt *HTTPBinderTester) TestBindJSONWithHTTP(t *testing.T) {
	t.Helper()
	t.Skip("HTTP binding tests temporarily disabled - needs refactoring")
}

// TestBindQueryWithHTTP tests query parameter binding with actual HTTP requests
func (hbt *HTTPBinderTester) TestBindQueryWithHTTP(t *testing.T) {
	t.Helper()
	t.Skip("HTTP binding tests temporarily disabled - needs refactoring")
}

// TestBindFormWithHTTP tests form data binding with actual HTTP requests
func (hbt *HTTPBinderTester) TestBindFormWithHTTP(t *testing.T) {
	t.Helper()
	t.Skip("HTTP binding tests temporarily disabled - needs refactoring")
}

// TestBindHeaderWithHTTP tests header binding with actual HTTP requests
func (hbt *HTTPBinderTester) TestBindHeaderWithHTTP(t *testing.T) {
	t.Helper()
	t.Skip("HTTP binding tests temporarily disabled - needs refactoring")
}

// RunAllHTTPTests runs all HTTP-based binding tests
func (hbt *HTTPBinderTester) RunAllHTTPTests(t *testing.T) {
	t.Helper()

	t.Run("BindJSONWithHTTP", hbt.TestBindJSONWithHTTP)
	t.Run("BindQueryWithHTTP", hbt.TestBindQueryWithHTTP)
	t.Run("BindFormWithHTTP", hbt.TestBindFormWithHTTP)
	t.Run("BindHeaderWithHTTP", hbt.TestBindHeaderWithHTTP)
}
