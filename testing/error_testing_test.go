package testing

import (
	"context"
	"errors"
	"io/fs"
	"testing"

	"github.com/go-sphere/httpx"
)

// MockEngine is a minimal mock implementation for testing
type MockEngine struct {
	groups []httpx.Router
}

func (m *MockEngine) Start() error                       { return nil }
func (m *MockEngine) Stop(ctx context.Context) error     { return nil }
func (m *MockEngine) IsRunning() bool                    { return false }
func (m *MockEngine) Use(middleware ...httpx.Middleware) {}
func (m *MockEngine) Group(prefix string, middleware ...httpx.Middleware) httpx.Router {
	router := &MockRouter{prefix: prefix}
	m.groups = append(m.groups, router)
	return router
}

// MockRouter is a minimal mock implementation for testing
type MockRouter struct {
	prefix string
	routes []MockRoute
}

type MockRoute struct {
	method  string
	path    string
	handler httpx.Handler
}

func (m *MockRouter) Handle(method, path string, handler httpx.Handler) {
	m.routes = append(m.routes, MockRoute{method: method, path: path, handler: handler})
}
func (m *MockRouter) GET(path string, handler httpx.Handler)     { m.Handle("GET", path, handler) }
func (m *MockRouter) POST(path string, handler httpx.Handler)    { m.Handle("POST", path, handler) }
func (m *MockRouter) PUT(path string, handler httpx.Handler)     { m.Handle("PUT", path, handler) }
func (m *MockRouter) DELETE(path string, handler httpx.Handler)  { m.Handle("DELETE", path, handler) }
func (m *MockRouter) PATCH(path string, handler httpx.Handler)   { m.Handle("PATCH", path, handler) }
func (m *MockRouter) HEAD(path string, handler httpx.Handler)    { m.Handle("HEAD", path, handler) }
func (m *MockRouter) OPTIONS(path string, handler httpx.Handler) { m.Handle("OPTIONS", path, handler) }
func (m *MockRouter) Any(path string, handler httpx.Handler)     { m.Handle("ANY", path, handler) }
func (m *MockRouter) Group(prefix string, middleware ...httpx.Middleware) httpx.Router {
	return &MockRouter{prefix: m.prefix + prefix}
}
func (m *MockRouter) Use(middleware ...httpx.Middleware)     {}
func (m *MockRouter) BasePath() string                       { return m.prefix }
func (m *MockRouter) Static(relativePath, root string)       {}
func (m *MockRouter) StaticFS(relativePath string, fs fs.FS) {}

func TestErrorTestingUtilities(t *testing.T) {
	engine := &MockEngine{}
	utils := NewErrorTestingUtilities(engine)

	t.Run("TestError creation", func(t *testing.T) {
		err := NewTestError("test message")
		AssertEqual(t, "test message", err.Error(), "Error message should match")
		AssertEqual(t, 500, err.StatusCode(), "Default status code should be 500")
	})

	t.Run("TestError with status", func(t *testing.T) {
		err := NewTestErrorWithStatus("bad request", 400)
		AssertEqual(t, "bad request", err.Error(), "Error message should match")
		AssertEqual(t, 400, err.StatusCode(), "Status code should match")
	})

	t.Run("TestError with cause", func(t *testing.T) {
		cause := errors.New("underlying error")
		err := NewTestErrorWithCause("wrapper error", cause)
		AssertEqual(t, "wrapper error: underlying error", err.Error(), "Error message should include cause")
		AssertEqual(t, cause, err.Unwrap(), "Unwrap should return cause")
	})

	t.Run("Handler creation", func(t *testing.T) {
		testErr := NewTestError("handler error")
		handler := utils.CreateErrorHandler(testErr)
		AssertNotEqual(t, nil, handler, "Handler should not be nil")

		successHandler := utils.CreateSuccessHandler("success")
		AssertNotEqual(t, nil, successHandler, "Success handler should not be nil")
	})

	t.Run("Middleware creation", func(t *testing.T) {
		testErr := NewTestError("middleware error")
		middleware := utils.CreateErrorMiddleware(testErr)
		AssertNotEqual(t, nil, middleware, "Error middleware should not be nil")

		passthroughMiddleware := utils.CreatePassthroughMiddleware()
		AssertNotEqual(t, nil, passthroughMiddleware, "Passthrough middleware should not be nil")

		recoveryMiddleware := utils.CreateErrorRecoveryMiddleware(func(err error) error {
			return nil // Convert error to success
		})
		AssertNotEqual(t, nil, recoveryMiddleware, "Recovery middleware should not be nil")
	})

	t.Run("Error testing scenarios", func(t *testing.T) {
		// Test that error testing methods don't panic
		utils.TestMiddlewareChainInterruption(t)
		utils.TestErrorRecoveryPatterns(t)
		utils.TestFrameworkAdapterErrorHandling(t)
		utils.RunAllErrorTests(t)
	})
}
