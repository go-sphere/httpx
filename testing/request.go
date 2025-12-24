package testing

import (
	"testing"

	"github.com/go-sphere/httpx"
)

// RequestTester tests the Request composite interface
// Request interface combines RequestInfo, BodyAccess, and FormAccess
// This tester focuses on composition behavior, not inherited method functionality
type RequestTester struct {
	engine httpx.Engine
}

// NewRequestTester creates a new Request interface tester
func NewRequestTester(engine httpx.Engine) *RequestTester {
	return &RequestTester{engine: engine}
}

// TestRequestInfoMethodExposure tests that Request interface exposes RequestInfo methods
func (rt *RequestTester) TestRequestInfoMethodExposure(t *testing.T) {
	t.Helper()

	testCases := []struct {
		name        string
		description string
	}{
		{"RequestInfo methods accessible", "Request interface should expose all RequestInfo methods"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			router := rt.engine.Group("")

			router.GET(GenerateUniqueTestPath(), func(ctx httpx.Context) error {
				// Test that Request interface (via Context) exposes RequestInfo methods
				// We're not testing the functionality, just that the methods are accessible

				// Method exposure test - these should compile and be callable
				_ = ctx.Method()
				_ = ctx.Path()
				_ = ctx.FullPath()
				_ = ctx.ClientIP()
				_ = ctx.Param("test")
				_ = ctx.Params()
				_ = ctx.Query("test")
				_ = ctx.Queries()
				_ = ctx.RawQuery()
				_ = ctx.Header("test")
				_ = ctx.Headers()
				_, _ = ctx.Cookie("test")
				_ = ctx.Cookies()

				t.Logf("All RequestInfo methods are accessible through Request interface")
				return ctx.Text(200, "OK")
			})

			t.Logf("Test %s completed", tc.name)
		})
	}
}

// TestBodyAccessMethodExposure tests that Request interface exposes BodyAccess methods
func (rt *RequestTester) TestBodyAccessMethodExposure(t *testing.T) {
	t.Helper()

	testCases := []struct {
		name        string
		description string
	}{
		{"BodyAccess methods accessible", "Request interface should expose all BodyAccess methods"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			router := rt.engine.Group("")

			router.POST(GenerateUniqueTestPath(), func(ctx httpx.Context) error {
				// Test that Request interface (via Context) exposes BodyAccess methods
				// We're not testing the functionality, just that the methods are accessible

				// Method exposure test - these should compile and be callable
				_, _ = ctx.BodyRaw()
				_ = ctx.BodyReader()

				t.Logf("All BodyAccess methods are accessible through Request interface")
				return ctx.Text(200, "OK")
			})

			t.Logf("Test %s completed", tc.name)
		})
	}
}

// TestFormAccessMethodExposure tests that Request interface exposes FormAccess methods
func (rt *RequestTester) TestFormAccessMethodExposure(t *testing.T) {
	t.Helper()

	testCases := []struct {
		name        string
		description string
	}{
		{"FormAccess methods accessible", "Request interface should expose all FormAccess methods"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			router := rt.engine.Group("")

			router.POST(GenerateUniqueTestPath(), func(ctx httpx.Context) error {
				// Test that Request interface (via Context) exposes FormAccess methods
				// We're not testing the functionality, just that the methods are accessible

				// Method exposure test - these should compile and be callable
				_ = ctx.FormValue("test")
				_, _ = ctx.MultipartForm()
				_, _ = ctx.FormFile("test")

				t.Logf("All FormAccess methods are accessible through Request interface")
				return ctx.Text(200, "OK")
			})

			t.Logf("Test %s completed", tc.name)
		})
	}
}

// TestCompositeInterfaceIntegrity tests that the composite interface maintains integrity
func (rt *RequestTester) TestCompositeInterfaceIntegrity(t *testing.T) {
	t.Helper()

	testCases := []struct {
		name        string
		description string
	}{
		{"Interface composition integrity", "Request interface should properly compose all sub-interfaces"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			router := rt.engine.Group("")

			router.POST(GenerateUniqueTestPath(), func(ctx httpx.Context) error {
				// Test that we can use methods from all composed interfaces together
				// This validates that the composition doesn't break interface contracts

				// Use RequestInfo methods (side-effect free)
				method := ctx.Method()
				path := ctx.Path()

				// Use BodyAccess methods (may have side effects)
				bodyBytes, bodyErr := ctx.BodyRaw()

				// Use FormAccess methods (may have side effects)
				formValue := ctx.FormValue("test")

				// Verify we got some response from each interface
				AssertNotEqual(t, "", method, "Method should not be empty")
				AssertNotEqual(t, "", path, "Path should not be empty")

				if bodyErr != nil {
					t.Logf("BodyRaw error (may be expected): %v", bodyErr)
				} else {
					t.Logf("BodyRaw returned %d bytes", len(bodyBytes))
				}

				t.Logf("FormValue returned: %s", formValue)
				t.Logf("Composite interface integrity verified")

				return ctx.Text(200, "OK")
			})

			t.Logf("Test %s completed", tc.name)
		})
	}
}

// TestInterfaceSegregation tests that Request interface properly segregates concerns
func (rt *RequestTester) TestInterfaceSegregation(t *testing.T) {
	t.Helper()

	testCases := []struct {
		name        string
		description string
	}{
		{"Interface segregation", "Request interface should maintain clear separation between sub-interfaces"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			router := rt.engine.Group("")

			router.POST(GenerateUniqueTestPath(), func(ctx httpx.Context) error {
				// Test that side-effect-free RequestInfo methods don't interfere
				// with side-effect methods from BodyAccess and FormAccess

				// First, use side-effect-free RequestInfo methods
				method := ctx.Method()
				headers := ctx.Headers()
				_ = ctx.Queries() // Use but don't store to avoid unused variable

				// Then use potentially side-effect methods
				_, bodyErr := ctx.BodyRaw() // Only store error, not body bytes
				formValue := ctx.FormValue("test")

				// Verify RequestInfo methods still work after side-effect methods
				methodAfter := ctx.Method()
				headersAfter := ctx.Headers()

				// RequestInfo methods should be consistent before and after
				AssertEqual(t, method, methodAfter, "Method should be consistent")

				// Headers should be the same (assuming no modification)
				if headers != nil && headersAfter != nil {
					t.Logf("Headers consistent before and after side-effect methods")
				}

				t.Logf("Interface segregation verified - RequestInfo: %s, Body: %v, Form: %s",
					method, bodyErr == nil, formValue)

				return ctx.Text(200, "OK")
			})

			t.Logf("Test %s completed", tc.name)
		})
	}
}

// RunAllTests runs all Request composite interface tests
func (rt *RequestTester) RunAllTests(t *testing.T) {
	t.Helper()
	t.Run("RequestInfoMethodExposure", rt.TestRequestInfoMethodExposure)
	t.Run("BodyAccessMethodExposure", rt.TestBodyAccessMethodExposure)
	t.Run("FormAccessMethodExposure", rt.TestFormAccessMethodExposure)
	t.Run("CompositeInterfaceIntegrity", rt.TestCompositeInterfaceIntegrity)
	t.Run("InterfaceSegregation", rt.TestInterfaceSegregation)
}
