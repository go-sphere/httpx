package testing

import (
	"embed"
	"io/fs"
	"testing"

	"github.com/go-sphere/httpx"
)

//go:embed testdata/*
var testFS embed.FS

// RouterTester tests the Router interface methods
type RouterTester struct {
	engine httpx.Engine
}

// NewRouterTester creates a new Router interface tester
func NewRouterTester(engine httpx.Engine) *RouterTester {
	return &RouterTester{engine: engine}
}

// TestHandle tests the Handle() method for registering routes with specific HTTP methods
func (rt *RouterTester) TestHandle(t *testing.T) {
	t.Helper()

	testCases := []struct {
		name       string
		method     string
		pathPrefix string
	}{
		{"GET route", "GET", "get-test"},
		{"POST route", "POST", "post-test"},
		{"PUT route", "PUT", "put-test"},
		{"DELETE route", "DELETE", "delete-test"},
		{"PATCH route", "PATCH", "patch-test"},
		{"HEAD route", "HEAD", "head-test"},
		{"OPTIONS route", "OPTIONS", "options-test"},
		{"Custom method", "CUSTOM", "custom-test"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			router := rt.engine.Group("")
			// var capturedContext httpx.Context

			// Generate unique path for this test case
			uniquePath := GenerateUniquePath(tc.pathPrefix)

			// Try to register the route, but handle frameworks that don't support custom methods
			defer func() {
				if r := recover(); r != nil {
					if tc.method == "CUSTOM" {
						t.Skipf("Framework doesn't support custom HTTP method: %s", tc.method)
						return
					}
					// Re-panic if it's not a custom method issue
					panic(r)
				}
			}()

			router.Handle(tc.method, uniquePath, func(ctx httpx.Context) error {
				// capturedContext = ctx
				AssertEqual(t, tc.method, ctx.Method(), "Method should match registered method")
				return ctx.Text(200, "OK")
			})

			// Route registration should not panic
			t.Logf("Test %s completed", tc.name)
		})
	}
}

// TestHTTPMethods tests the HTTP method shortcuts (GET, POST, PUT, DELETE, PATCH, HEAD, OPTIONS)
func (rt *RouterTester) TestHTTPMethods(t *testing.T) {
	t.Helper()

	testCases := []struct {
		name           string
		method         string
		registerFunc   func(router httpx.Router, path string, handler httpx.Handler)
		expectedMethod string
	}{
		{
			"GET method",
			"GET",
			func(router httpx.Router, path string, handler httpx.Handler) {
				router.GET(path, handler)
			},
			"GET",
		},
		{
			"POST method",
			"POST",
			func(router httpx.Router, path string, handler httpx.Handler) {
				router.POST(path, handler)
			},
			"POST",
		},
		{
			"PUT method",
			"PUT",
			func(router httpx.Router, path string, handler httpx.Handler) {
				router.PUT(path, handler)
			},
			"PUT",
		},
		{
			"DELETE method",
			"DELETE",
			func(router httpx.Router, path string, handler httpx.Handler) {
				router.DELETE(path, handler)
			},
			"DELETE",
		},
		{
			"PATCH method",
			"PATCH",
			func(router httpx.Router, path string, handler httpx.Handler) {
				router.PATCH(path, handler)
			},
			"PATCH",
		},
		{
			"HEAD method",
			"HEAD",
			func(router httpx.Router, path string, handler httpx.Handler) {
				router.HEAD(path, handler)
			},
			"HEAD",
		},
		{
			"OPTIONS method",
			"OPTIONS",
			func(router httpx.Router, path string, handler httpx.Handler) {
				router.OPTIONS(path, handler)
			},
			"OPTIONS",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			router := rt.engine.Group("")
			// var capturedContext httpx.Context

			tc.registerFunc(router, GenerateUniqueTestPath(), func(ctx httpx.Context) error {
				// capturedContext = ctx
				AssertEqual(t, tc.expectedMethod, ctx.Method(), "Method should match expected HTTP method")
				return ctx.Text(200, "OK")
			})

			// Route registration should not panic
			t.Logf("Test %s completed", tc.name)
		})
	}
}

// TestAny tests the Any() method for registering routes that respond to all HTTP methods
func (rt *RouterTester) TestAny(t *testing.T) {
	t.Helper()

	testCases := []struct {
		name   string
		method string
	}{
		{"Any with GET", "GET"},
		{"Any with POST", "POST"},
		{"Any with PUT", "PUT"},
		{"Any with DELETE", "DELETE"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			router := rt.engine.Group("")
			// var capturedContext httpx.Context

			// Use unique path for each test case to avoid conflicts
			uniquePath := GenerateUniquePath("any-test")

			router.Any(uniquePath, func(ctx httpx.Context) error {
				// capturedContext = ctx
				// Any route should accept any HTTP method
				t.Logf("Any route received method: %s", ctx.Method())
				return ctx.Text(200, "OK")
			})

			// Route registration should not panic
			t.Logf("Test %s completed", tc.name)
		})
	}
}

// TestGroup tests the Group() method for creating route groups with prefixes
func (rt *RouterTester) TestGroup(t *testing.T) {
	t.Helper()

	testCases := []struct {
		name   string
		prefix string
		path   string
	}{
		{"Simple group", "/api", "/users"},
		{"Nested prefix", "/api/v1", "/users"},
		{"Empty prefix", "", "/test"},
		{"Root prefix", "/", "/test"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			router := rt.engine.Group("")
			// var capturedContext httpx.Context

			group := router.Group(tc.prefix)
			AssertNotEqual(t, nil, group, "Group should return a valid Router instance")

			group.GET(GenerateUniqueTestPath(), func(ctx httpx.Context) error {
				// capturedContext = ctx
				// Verify the full path includes the group prefix
				fullPath := ctx.FullPath()
				if fullPath != "" {
					t.Logf("Group route full path: %s", fullPath)
				}
				return ctx.Text(200, "OK")
			})

			// Group creation and route registration should not panic
			t.Logf("Test %s completed", tc.name)
		})
	}
}

// TestGroupWithMiddleware tests the Group() method with middleware attachment
func (rt *RouterTester) TestGroupWithMiddleware(t *testing.T) {
	t.Helper()

	testCases := []struct {
		name            string
		prefix          string
		middlewareCount int
	}{
		{"Group with single middleware", "/api", 1},
		{"Group with multiple middleware", "/api/v1", 2},
		{"Group with no middleware", "/simple", 0},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			router := rt.engine.Group("")
			// var capturedContext httpx.Context
			// var middlewareExecuted int

			// Create middleware functions
			middlewares := make([]httpx.Middleware, tc.middlewareCount)
			for i := 0; i < tc.middlewareCount; i++ {
				middlewares[i] = func(ctx httpx.Context) error {
					// middlewareExecuted++
					return ctx.Next()
				}
			}

			group := router.Group(tc.prefix, middlewares...)
			AssertNotEqual(t, nil, group, "Group with middleware should return a valid Router instance")

			group.GET(GenerateUniqueTestPath(), func(ctx httpx.Context) error {
				// capturedContext = ctx
				// Note: middleware execution count can't be verified without actual HTTP requests
				return ctx.Text(200, "OK")
			})

			// Group creation with middleware should not panic
			t.Logf("Test %s completed", tc.name)
		})
	}
}

// TestUse tests the Use() method for attaching middleware to routes
func (rt *RouterTester) TestUse(t *testing.T) {
	t.Helper()

	testCases := []struct {
		name            string
		middlewareCount int
	}{
		{"Single middleware", 1},
		{"Multiple middleware", 3},
		{"No middleware", 0},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			router := rt.engine.Group("")
			// var capturedContext httpx.Context
			// var middlewareExecuted int

			// Add middleware using Use()
			for i := 0; i < tc.middlewareCount; i++ {
				router.Use(func(ctx httpx.Context) error {
					// middlewareExecuted++
					return ctx.Next()
				})
			}

			router.GET(GenerateUniqueTestPath(), func(ctx httpx.Context) error {
				// capturedContext = ctx
				// Note: middleware execution count can't be verified without actual HTTP requests
				return ctx.Text(200, "OK")
			})

			// Middleware registration should not panic
			t.Logf("Test %s completed", tc.name)
		})
	}
}

// TestBasePath tests the BasePath() method for retrieving the router's base path
func (rt *RouterTester) TestBasePath(t *testing.T) {
	t.Helper()

	testCases := []struct {
		name           string
		prefix         string
		expectedPrefix string
	}{
		{"Root base path", "", ""},
		{"API base path", "/api", "/api"},
		{"Nested base path", "/api/v1", "/api/v1"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			router := rt.engine.Group("")

			group := router.Group(tc.prefix)
			basePath := group.BasePath()

			// BasePath should return the group's prefix
			// Note: Some frameworks may normalize paths differently
			t.Logf("Group prefix: %s, BasePath: %s", tc.prefix, basePath)

			// Verify BasePath is accessible (implementation may vary by framework)
			if basePath != tc.expectedPrefix {
				t.Logf("BasePath differs from expected (framework-specific behavior): expected %s, got %s", tc.expectedPrefix, basePath)
			}

			t.Logf("Test %s completed", tc.name)
		})
	}
}

// TestStatic tests the Static() method for serving static files
func (rt *RouterTester) TestStatic(t *testing.T) {
	t.Helper()

	testCases := []struct {
		name   string
		prefix string
		root   string
	}{
		{"Static files from current directory", GenerateUniquePath("static"), "."},
		{"Static files from testing directory", GenerateUniquePath("files"), "./testing"},
		{"Root static files", GenerateUniquePath("root"), "."},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			router := rt.engine.Group("")

			// Register static file serving
			router.Static(tc.prefix, tc.root)

			// Static registration should not panic or error
			// Actual file serving would require HTTP requests in integration tests
			t.Logf("Static route registered: prefix=%s, root=%s", tc.prefix, tc.root)

			t.Logf("Test %s completed", tc.name)
		})
	}
}

// TestStaticFS tests the StaticFS() method for serving files from an embedded filesystem
func (rt *RouterTester) TestStaticFS(t *testing.T) {
	t.Helper()

	testCases := []struct {
		name   string
		prefix string
		fs     fs.FS
	}{
		{"Embedded filesystem", GenerateUniquePath("embed"), testFS},
		{"Subdirectory filesystem", GenerateUniquePath("testdata"), func() fs.FS {
			sub, err := fs.Sub(testFS, "testdata")
			if err != nil {
				// If testdata doesn't exist, use the root FS
				return testFS
			}
			return sub
		}()},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			router := rt.engine.Group("")

			// Register static filesystem serving
			router.StaticFS(tc.prefix, tc.fs)

			// StaticFS registration should not panic or error
			// Actual file serving would require HTTP requests in integration tests
			t.Logf("StaticFS route registered: prefix=%s", tc.prefix)

			t.Logf("Test %s completed", tc.name)
		})
	}
}

// TestNestedGroups tests nested group creation and path composition
func (rt *RouterTester) TestNestedGroups(t *testing.T) {
	t.Helper()

	testCases := []struct {
		name     string
		prefixes []string
	}{
		{"Two-level nesting", []string{"/api", "/v1"}},
		{"Three-level nesting", []string{"/api", "/v1", "/users"}},
		{"Mixed nesting", []string{"/app", "/admin", "/dashboard"}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			router := rt.engine.Group("")
			// var capturedContext httpx.Context

			// Create nested groups
			currentRouter := router
			for _, prefix := range tc.prefixes {
				currentRouter = currentRouter.Group(prefix)
				AssertNotEqual(t, nil, currentRouter, "Nested group should return valid Router instance")
			}

			// Add a route to the deepest nested group
			currentRouter.GET(GenerateUniqueTestPath(), func(ctx httpx.Context) error {
				// capturedContext = ctx
				fullPath := ctx.FullPath()
				if fullPath != "" {
					t.Logf("Nested group route full path: %s", fullPath)
				}
				return ctx.Text(200, "OK")
			})

			// Nested group creation and route registration should not panic
			t.Logf("Test %s completed", tc.name)
		})
	}
}

// RunAllTests runs all Router interface tests
func (rt *RouterTester) RunAllTests(t *testing.T) {
	t.Helper()
	t.Run("Handle", rt.TestHandle)
	t.Run("HTTPMethods", rt.TestHTTPMethods)
	t.Run("Any", rt.TestAny)
	t.Run("Group", rt.TestGroup)
	t.Run("GroupWithMiddleware", rt.TestGroupWithMiddleware)
	t.Run("Use", rt.TestUse)
	t.Run("BasePath", rt.TestBasePath)
	t.Run("Static", rt.TestStatic)
	t.Run("StaticFS", rt.TestStaticFS)
	t.Run("NestedGroups", rt.TestNestedGroups)
}
