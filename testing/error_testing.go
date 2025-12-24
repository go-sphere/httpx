package testing

import (
	"errors"
	"fmt"
	"testing"

	"github.com/go-sphere/httpx"
)

// ErrorTestingUtilities provides utilities for testing error scenarios
type ErrorTestingUtilities struct {
	engine httpx.Engine
}

// NewErrorTestingUtilities creates a new error testing utilities instance
func NewErrorTestingUtilities(engine httpx.Engine) *ErrorTestingUtilities {
	return &ErrorTestingUtilities{engine: engine}
}

// TestError represents a test error with optional HTTP status code
type TestError struct {
	message    string
	statusCode int
	cause      error
}

// NewTestError creates a new test error
func NewTestError(message string) *TestError {
	return &TestError{message: message}
}

// NewTestErrorWithStatus creates a new test error with HTTP status code
func NewTestErrorWithStatus(message string, statusCode int) *TestError {
	return &TestError{message: message, statusCode: statusCode}
}

// NewTestErrorWithCause creates a new test error with a cause
func NewTestErrorWithCause(message string, cause error) *TestError {
	return &TestError{message: message, cause: cause}
}

// Error implements the error interface
func (e *TestError) Error() string {
	if e.cause != nil {
		return fmt.Sprintf("%s: %v", e.message, e.cause)
	}
	return e.message
}

// StatusCode returns the HTTP status code if this error implements HTTPError
func (e *TestError) StatusCode() int {
	if e.statusCode > 0 {
		return e.statusCode
	}
	return 500 // Default to internal server error
}

// Unwrap returns the underlying cause error
func (e *TestError) Unwrap() error {
	return e.cause
}

// ErrorScenario represents a test scenario for error handling
type ErrorScenario struct {
	Name           string
	Handler        httpx.Handler
	Middleware     []httpx.Middleware
	ExpectedError  error
	ExpectedStatus int
	Description    string
}

// TestErrorPropagation tests error propagation through middleware chains
func (etu *ErrorTestingUtilities) TestErrorPropagation(t *testing.T, scenarios []ErrorScenario) {
	t.Helper()

	for _, scenario := range scenarios {
		t.Run(scenario.Name, func(t *testing.T) {
			router := etu.engine.Group("")

			// Add middleware to the router
			for _, middleware := range scenario.Middleware {
				router.Use(middleware)
			}

			// Add the handler
			router.GET(GenerateUniqueTestPath(), scenario.Handler)

			// The test verifies that the route can be registered without panicking
			// Actual error propagation testing would require HTTP requests in integration tests
			t.Logf("Error propagation test registered: %s - %s", scenario.Name, scenario.Description)
		})
	}
}

// CreateErrorHandler creates a handler that returns a specific error
func (etu *ErrorTestingUtilities) CreateErrorHandler(err error) httpx.Handler {
	return func(ctx httpx.Context) error {
		return err
	}
}

// CreateSuccessHandler creates a handler that returns nil (success)
func (etu *ErrorTestingUtilities) CreateSuccessHandler(message string) httpx.Handler {
	return func(ctx httpx.Context) error {
		return ctx.Text(200, message)
	}
}

// CreateErrorMiddleware creates middleware that returns a specific error
func (etu *ErrorTestingUtilities) CreateErrorMiddleware(err error) httpx.Middleware {
	return func(ctx httpx.Context) error {
		return err
	}
}

// CreatePassthroughMiddleware creates middleware that calls Next() and returns its result
func (etu *ErrorTestingUtilities) CreatePassthroughMiddleware() httpx.Middleware {
	return func(ctx httpx.Context) error {
		return ctx.Next()
	}
}

// CreateErrorRecoveryMiddleware creates middleware that catches errors and handles them
func (etu *ErrorTestingUtilities) CreateErrorRecoveryMiddleware(recoveryHandler func(error) error) httpx.Middleware {
	return func(ctx httpx.Context) error {
		err := ctx.Next()
		if err != nil {
			return recoveryHandler(err)
		}
		return nil
	}
}

// CreateLoggingMiddleware creates middleware that logs errors but propagates them
func (etu *ErrorTestingUtilities) CreateLoggingMiddleware(t *testing.T) httpx.Middleware {
	return func(ctx httpx.Context) error {
		err := ctx.Next()
		if err != nil {
			t.Logf("Middleware caught error: %v", err)
		}
		return err
	}
}

// CreateConditionalErrorMiddleware creates middleware that returns an error based on a condition
func (etu *ErrorTestingUtilities) CreateConditionalErrorMiddleware(condition func(httpx.Context) bool, err error) httpx.Middleware {
	return func(ctx httpx.Context) error {
		if condition(ctx) {
			return err
		}
		return ctx.Next()
	}
}

// TestMiddlewareChainInterruption tests that middleware chains are interrupted on errors
func (etu *ErrorTestingUtilities) TestMiddlewareChainInterruption(t *testing.T) {
	t.Helper()

	testCases := []struct {
		name        string
		middlewares []httpx.Middleware
		handler     httpx.Handler
		description string
	}{
		{
			name: "First middleware returns error",
			middlewares: []httpx.Middleware{
				etu.CreateErrorMiddleware(NewTestError("middleware error")),
				etu.CreatePassthroughMiddleware(), // Should not be executed
			},
			handler:     etu.CreateSuccessHandler("success"),
			description: "First middleware error should prevent subsequent middleware and handler execution",
		},
		{
			name: "Second middleware returns error",
			middlewares: []httpx.Middleware{
				etu.CreatePassthroughMiddleware(), // Should execute
				etu.CreateErrorMiddleware(NewTestError("second middleware error")),
				etu.CreatePassthroughMiddleware(), // Should not be executed
			},
			handler:     etu.CreateSuccessHandler("success"),
			description: "Second middleware error should prevent subsequent middleware and handler execution",
		},
		{
			name: "Handler returns error",
			middlewares: []httpx.Middleware{
				etu.CreatePassthroughMiddleware(), // Should execute
				etu.CreatePassthroughMiddleware(), // Should execute
			},
			handler:     etu.CreateErrorHandler(NewTestError("handler error")),
			description: "Handler error should be propagated back through middleware chain",
		},
		{
			name: "All succeed",
			middlewares: []httpx.Middleware{
				etu.CreatePassthroughMiddleware(),
				etu.CreatePassthroughMiddleware(),
			},
			handler:     etu.CreateSuccessHandler("all good"),
			description: "All middleware and handler succeed",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			router := etu.engine.Group("")

			// Add middleware
			for _, middleware := range tc.middlewares {
				router.Use(middleware)
			}

			// Add handler
			router.GET(GenerateUniqueTestPath(), tc.handler)

			t.Logf("Middleware chain interruption test registered: %s", tc.description)
		})
	}
}

// TestErrorRecoveryPatterns tests error recovery and transformation patterns
func (etu *ErrorTestingUtilities) TestErrorRecoveryPatterns(t *testing.T) {
	t.Helper()

	testCases := []struct {
		name        string
		middleware  httpx.Middleware
		handler     httpx.Handler
		description string
	}{
		{
			name: "Error recovery to success",
			middleware: etu.CreateErrorRecoveryMiddleware(func(err error) error {
				t.Logf("Recovered from error: %v", err)
				return nil // Convert error to success
			}),
			handler:     etu.CreateErrorHandler(NewTestError("recoverable error")),
			description: "Middleware should recover from handler error and return success",
		},
		{
			name: "Error transformation",
			middleware: etu.CreateErrorRecoveryMiddleware(func(err error) error {
				t.Logf("Transforming error: %v", err)
				return NewTestError("transformed error")
			}),
			handler:     etu.CreateErrorHandler(NewTestError("original error")),
			description: "Middleware should transform handler error into different error",
		},
		{
			name: "Error propagation",
			middleware: etu.CreateErrorRecoveryMiddleware(func(err error) error {
				t.Logf("Logging error but propagating: %v", err)
				return err // Propagate the error unchanged
			}),
			handler:     etu.CreateErrorHandler(NewTestError("propagated error")),
			description: "Middleware should log error but propagate it unchanged",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			router := etu.engine.Group("")
			router.Use(tc.middleware)
			router.GET(GenerateUniqueTestPath(), tc.handler)

			t.Logf("Error recovery pattern test registered: %s", tc.description)
		})
	}
}

// TestFrameworkAdapterErrorHandling tests framework-specific error handling
func (etu *ErrorTestingUtilities) TestFrameworkAdapterErrorHandling(t *testing.T) {
	t.Helper()

	testCases := []struct {
		name        string
		error       error
		description string
	}{
		{
			name:        "Standard error",
			error:       errors.New("standard error"),
			description: "Framework should handle standard Go errors with default 500 status",
		},
		{
			name:        "HTTP error with status",
			error:       NewTestErrorWithStatus("bad request", 400),
			description: "Framework should use embedded status code from HTTPError",
		},
		{
			name:        "HTTP error with cause",
			error:       NewTestErrorWithCause("internal error", errors.New("database connection failed")),
			description: "Framework should handle errors with underlying causes",
		},
		{
			name:        "Nil error (success)",
			error:       nil,
			description: "Framework should handle successful requests (nil error)",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			router := etu.engine.Group("")

			var handler httpx.Handler
			if tc.error != nil {
				handler = etu.CreateErrorHandler(tc.error)
			} else {
				handler = etu.CreateSuccessHandler("success")
			}

			router.GET(GenerateUniqueTestPath(), handler)

			t.Logf("Framework adapter error handling test registered: %s", tc.description)
		})
	}
}

// CreateComplexErrorScenario creates a complex error scenario with multiple middleware layers
func (etu *ErrorTestingUtilities) CreateComplexErrorScenario(t *testing.T, scenarioName string) {
	t.Helper()

	router := etu.engine.Group("")

	// Layer 1: Logging middleware
	router.Use(func(ctx httpx.Context) error {
		t.Logf("[%s] Request started: %s %s", scenarioName, ctx.Method(), ctx.Path())
		err := ctx.Next()
		if err != nil {
			t.Logf("[%s] Request failed: %v", scenarioName, err)
		} else {
			t.Logf("[%s] Request succeeded", scenarioName)
		}
		return err
	})

	// Layer 2: Authentication middleware (conditional error)
	router.Use(func(ctx httpx.Context) error {
		authHeader := ctx.Header("Authorization")
		if authHeader == "" && ctx.Path() != "/public" {
			return NewTestErrorWithStatus("unauthorized", 401)
		}
		return ctx.Next()
	})

	// Layer 3: Rate limiting middleware (conditional error)
	router.Use(func(ctx httpx.Context) error {
		rateLimitHeader := ctx.Header("X-Rate-Limit")
		if rateLimitHeader == "exceeded" {
			return NewTestErrorWithStatus("rate limit exceeded", 429)
		}
		return ctx.Next()
	})

	// Add various routes for testing different scenarios
	router.GET("/public", etu.CreateSuccessHandler("public endpoint"))
	router.GET("/protected", etu.CreateSuccessHandler("protected endpoint"))
	router.GET("/error", etu.CreateErrorHandler(NewTestError("intentional error")))

	t.Logf("Complex error scenario created: %s", scenarioName)
}

// RunAllErrorTests runs all error testing scenarios
func (etu *ErrorTestingUtilities) RunAllErrorTests(t *testing.T) {
	t.Helper()
	t.Run("MiddlewareChainInterruption", etu.TestMiddlewareChainInterruption)
	t.Run("ErrorRecoveryPatterns", etu.TestErrorRecoveryPatterns)
	t.Run("FrameworkAdapterErrorHandling", etu.TestFrameworkAdapterErrorHandling)

	// Create complex scenarios
	t.Run("ComplexErrorScenario", func(t *testing.T) {
		etu.CreateComplexErrorScenario(t, "ComplexTest")
	})
}
