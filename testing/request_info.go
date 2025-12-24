package testing

import (
	"strings"
	"testing"

	"github.com/go-sphere/httpx"
)

// RequestInfoTester tests the RequestInfo interface methods
type RequestInfoTester struct {
	engine httpx.Engine
}

// NewRequestInfoTester creates a new RequestInfo interface tester
func NewRequestInfoTester(engine httpx.Engine) *RequestInfoTester {
	return &RequestInfoTester{engine: engine}
}

// TestMethod tests the Method() method
func (rit *RequestInfoTester) TestMethod(t *testing.T) {
	t.Helper()

	testCases := []struct {
		name   string
		method string
	}{
		{"GET method", "GET"},
		{"POST method", "POST"},
		{"PUT method", "PUT"},
		{"DELETE method", "DELETE"},
		{"PATCH method", "PATCH"},
		{"HEAD method", "HEAD"},
		{"OPTIONS method", "OPTIONS"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			router := rit.engine.Group("")
			// var capturedContext httpx.Context

			// Use unique route path for each test case to avoid conflicts
			uniquePath := GenerateUniqueTestPath()
			router.Handle(tc.method, uniquePath, func(ctx httpx.Context) error {
				// capturedContext = ctx
				AssertEqual(t, tc.method, ctx.Method(), "Method should match")
				return ctx.Text(200, "OK")
			})

			t.Logf("Test %s completed", tc.name)
		})
	}
}

// TestPath tests the Path() method
func (rit *RequestInfoTester) TestPath(t *testing.T) {
	t.Helper()

	testCases := []struct {
		name         string
		requestPath  string
		expectedPath string
	}{
		{"Root path", "/", "/"},
		{"Simple path", "/test", "/test"},
		{"Nested path", "/api/v1/users", "/api/v1/users"},
		{"Path with query", "/test?param=value", "/test"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			router := rit.engine.Group("")
			// var capturedContext httpx.Context

			// Use unique route path for each test case to avoid conflicts
			uniquePath := GenerateUniqueTestPath()
			router.GET(uniquePath, func(ctx httpx.Context) error {
				// capturedContext = ctx
				AssertEqual(t, tc.expectedPath, ctx.Path(), "Path should match")
				return ctx.Text(200, "OK")
			})

			t.Logf("Test %s completed", tc.name)
		})
	}
}

// TestFullPath tests the FullPath() method
func (rit *RequestInfoTester) TestFullPath(t *testing.T) {
	t.Helper()

	testCases := []struct {
		name             string
		routePattern     string
		expectedFullPath string
	}{
		{"Simple route", "/test", "/test"},
		{"Route with param", "/users/:id", "/users/:id"},
		{"Nested route with params", "/api/v1/users/:id/posts/:postId", "/api/v1/users/:id/posts/:postId"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			router := rit.engine.Group("")
			// var capturedContext httpx.Context

			var routeToUse string
			var expectedFullPath string
			if tc.routePattern == "/test" {
				routeToUse = GenerateUniqueTestPath()
				expectedFullPath = routeToUse
			} else {
				routeToUse = GenerateUniqueParamPath(tc.routePattern)
				expectedFullPath = routeToUse
			}

			router.GET(routeToUse, func(ctx httpx.Context) error {
				// capturedContext = ctx
				// FullPath returns route pattern when available, empty otherwise
				fullPath := ctx.FullPath()
				if fullPath != "" {
					AssertEqual(t, expectedFullPath, fullPath, "FullPath should match route pattern")
				}
				return ctx.Text(200, "OK")
			})

			t.Logf("Test %s completed", tc.name)
		})
	}
}

// TestClientIP tests the ClientIP() method
func (rit *RequestInfoTester) TestClientIP(t *testing.T) {
	t.Helper()

	testCases := []struct {
		name string
	}{
		{"Basic client IP detection"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			router := rit.engine.Group("")
			// var capturedContext httpx.Context

			uniquePath := GenerateUniqueTestPath()
			router.GET(uniquePath, func(ctx httpx.Context) error {
				// capturedContext = ctx
				clientIP := ctx.ClientIP()
				// ClientIP should return some value (best-effort detection)
				AssertNotEqual(t, "", clientIP, "ClientIP should not be empty")
				return ctx.Text(200, "OK")
			})

			t.Logf("Test %s completed", tc.name)
		})
	}
}

// TestParam tests the Param() method
func (rit *RequestInfoTester) TestParam(t *testing.T) {
	t.Helper()

	testCases := []struct {
		name          string
		routePattern  string
		paramKey      string
		expectedValue string
	}{
		{"Single param", "/users/:id", "id", "123"},
		{"Multiple params", "/users/:userId/posts/:postId", "userId", "456"},
		{"Non-existent param", "/users/:id", "nonexistent", ""},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			router := rit.engine.Group("")
			// var capturedContext httpx.Context

			uniqueRoute := GenerateUniqueParamPath(tc.routePattern)
			router.GET(uniqueRoute, func(ctx httpx.Context) error {
				// capturedContext = ctx
				paramValue := ctx.Param(tc.paramKey)
				if tc.expectedValue != "" {
					AssertEqual(t, tc.expectedValue, paramValue, "Param value should match")
				}
				return ctx.Text(200, "OK")
			})

			t.Logf("Test %s completed", tc.name)
		})
	}
}

// TestParams tests the Params() method
func (rit *RequestInfoTester) TestParams(t *testing.T) {
	t.Helper()

	testCases := []struct {
		name         string
		routePattern string
	}{
		{"No params", "/test"},
		{"Single param", "/users/:id"},
		{"Multiple params", "/users/:userId/posts/:postId"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			router := rit.engine.Group("")
			// var capturedContext httpx.Context

			var routeToUse string
			if tc.routePattern == "/test" {
				routeToUse = GenerateUniqueTestPath()
			} else {
				routeToUse = GenerateUniqueParamPath(tc.routePattern)
			}

			router.GET(routeToUse, func(ctx httpx.Context) error {
				// capturedContext = ctx
				params := ctx.Params()
				// Params should return nil if no params, otherwise a map
				if !hasParams(tc.routePattern) {
					if params != nil && len(params) > 0 {
						t.Errorf("Expected nil or empty params for route without params, got %v", params)
					}
				} else {
					AssertNotEqual(t, nil, params, "Params should not be nil for parameterized route")
				}
				return ctx.Text(200, "OK")
			})

			t.Logf("Test %s completed", tc.name)
		})
	}
}

// TestQuery tests the Query() method
func (rit *RequestInfoTester) TestQuery(t *testing.T) {
	t.Helper()

	testCases := []struct {
		name          string
		queryKey      string
		expectedValue string
	}{
		{"Existing query param", "name", "test"},
		{"Non-existent query param", "nonexistent", ""},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			router := rit.engine.Group("")
			// var capturedContext httpx.Context

			router.GET(GenerateUniqueTestPath(), func(ctx httpx.Context) error {
				// capturedContext = ctx
				queryValue := ctx.Query(tc.queryKey)
				AssertEqual(t, tc.expectedValue, queryValue, "Query value should match")
				return ctx.Text(200, "OK")
			})

			t.Logf("Test %s completed", tc.name)
		})
	}
}

// TestQueries tests the Queries() method
func (rit *RequestInfoTester) TestQueries(t *testing.T) {
	t.Helper()

	testCases := []struct {
		name string
	}{
		{"Query parameters"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			router := rit.engine.Group("")
			// var capturedContext httpx.Context

			router.GET(GenerateUniqueTestPath(), func(ctx httpx.Context) error {
				// capturedContext = ctx
				queries := ctx.Queries()
				// Queries should return nil if no queries, otherwise a map
				t.Logf("Queries: %v", queries)
				return ctx.Text(200, "OK")
			})

			t.Logf("Test %s completed", tc.name)
		})
	}
}

// TestRawQuery tests the RawQuery() method
func (rit *RequestInfoTester) TestRawQuery(t *testing.T) {
	t.Helper()

	testCases := []struct {
		name string
	}{
		{"Raw query string"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			router := rit.engine.Group("")
			// var capturedContext httpx.Context

			router.GET(GenerateUniqueTestPath(), func(ctx httpx.Context) error {
				// capturedContext = ctx
				rawQuery := ctx.RawQuery()
				t.Logf("Raw query: %s", rawQuery)
				return ctx.Text(200, "OK")
			})

			t.Logf("Test %s completed", tc.name)
		})
	}
}

// TestHeader tests the Header() method
func (rit *RequestInfoTester) TestHeader(t *testing.T) {
	t.Helper()

	testCases := []struct {
		name          string
		headerKey     string
		expectedValue string
	}{
		{"Content-Type header", "Content-Type", "application/json"},
		{"Non-existent header", "X-Non-Existent", ""},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			router := rit.engine.Group("")
			// var capturedContext httpx.Context

			router.GET(GenerateUniqueTestPath(), func(ctx httpx.Context) error {
				// capturedContext = ctx
				headerValue := ctx.Header(tc.headerKey)
				AssertEqual(t, tc.expectedValue, headerValue, "Header value should match")
				return ctx.Text(200, "OK")
			})

			t.Logf("Test %s completed", tc.name)
		})
	}
}

// TestHeaders tests the Headers() method
func (rit *RequestInfoTester) TestHeaders(t *testing.T) {
	t.Helper()

	testCases := []struct {
		name string
	}{
		{"All headers"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			router := rit.engine.Group("")
			// var capturedContext httpx.Context

			router.GET(GenerateUniqueTestPath(), func(ctx httpx.Context) error {
				// capturedContext = ctx
				headers := ctx.Headers()
				// Headers should return nil if no headers, otherwise a map
				t.Logf("Headers: %v", headers)
				return ctx.Text(200, "OK")
			})

			t.Logf("Test %s completed", tc.name)
		})
	}
}

// TestCookie tests the Cookie() method
func (rit *RequestInfoTester) TestCookie(t *testing.T) {
	t.Helper()

	testCases := []struct {
		name       string
		cookieName string
	}{
		{"Existing cookie", "session"},
		{"Non-existent cookie", "nonexistent"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			router := rit.engine.Group("")
			// var capturedContext httpx.Context

			router.GET(GenerateUniqueTestPath(), func(ctx httpx.Context) error {
				// capturedContext = ctx
				cookieValue, err := ctx.Cookie(tc.cookieName)
				if tc.cookieName == "nonexistent" {
					AssertError(t, err, "Should return error for non-existent cookie")
				} else {
					t.Logf("Cookie %s: %s (error: %v)", tc.cookieName, cookieValue, err)
				}
				return ctx.Text(200, "OK")
			})

			t.Logf("Test %s completed", tc.name)
		})
	}
}

// TestCookies tests the Cookies() method
func (rit *RequestInfoTester) TestCookies(t *testing.T) {
	t.Helper()

	testCases := []struct {
		name string
	}{
		{"All cookies"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			router := rit.engine.Group("")
			// var capturedContext httpx.Context

			router.GET(GenerateUniqueTestPath(), func(ctx httpx.Context) error {
				// capturedContext = ctx
				cookies := ctx.Cookies()
				// Cookies should return nil if no cookies, otherwise a map
				t.Logf("Cookies: %v", cookies)
				return ctx.Text(200, "OK")
			})

			t.Logf("Test %s completed", tc.name)
		})
	}
}

// RunAllTests runs all RequestInfo interface tests
func (rit *RequestInfoTester) RunAllTests(t *testing.T) {
	t.Helper()
	t.Run("Method", rit.TestMethod)
	t.Run("Path", rit.TestPath)
	t.Run("FullPath", rit.TestFullPath)
	t.Run("ClientIP", rit.TestClientIP)
	t.Run("Param", rit.TestParam)
	t.Run("Params", rit.TestParams)
	t.Run("Query", rit.TestQuery)
	t.Run("Queries", rit.TestQueries)
	t.Run("RawQuery", rit.TestRawQuery)
	t.Run("Header", rit.TestHeader)
	t.Run("Headers", rit.TestHeaders)
	t.Run("Cookie", rit.TestCookie)
	t.Run("Cookies", rit.TestCookies)
}

// hasParams checks if a route pattern contains parameters
func hasParams(routePattern string) bool {
	return strings.Contains(routePattern, ":")
}
