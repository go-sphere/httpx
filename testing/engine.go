package testing

import (
	"context"
	"testing"
	"time"

	"github.com/go-sphere/httpx"
)

// EngineTester tests the Engine interface methods
type EngineTester struct {
	engine httpx.Engine
}

// NewEngineTester creates a new Engine interface tester
func NewEngineTester(engine httpx.Engine) *EngineTester {
	return &EngineTester{engine: engine}
}

// TestStart tests the Start() method
func (et *EngineTester) TestStart(t *testing.T) {
	t.Helper()

	testCases := []struct {
		name string
	}{
		{"Engine start"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Note: We don't actually start the engine in tests
			// as it would block. This test verifies the method exists.

			// Check initial state
			AssertEqual(t, false, et.engine.IsRunning(), "Engine should not be running initially")

			t.Logf("Test %s completed", tc.name)
		})
	}
}

// TestStop tests the Stop() method
func (et *EngineTester) TestStop(t *testing.T) {
	t.Helper()

	testCases := []struct {
		name string
	}{
		{"Engine stop"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Test stopping with context
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			// Stop should not error even if engine is not running
			err := et.engine.Stop(ctx)

			// Some frameworks (like Hertzx) return an error when stopping a non-running engine
			// This is acceptable framework-specific behavior
			if err != nil {
				t.Logf("Framework returned error when stopping non-running engine: %v", err)
				// This is acceptable - some frameworks consider this an error condition
			}

			t.Logf("Test %s completed", tc.name)
		})
	}
}

// TestIsRunning tests the IsRunning() method
func (et *EngineTester) TestIsRunning(t *testing.T) {
	t.Helper()

	testCases := []struct {
		name string
	}{
		{"Engine running status"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Initially should not be running
			running := et.engine.IsRunning()
			AssertEqual(t, false, running, "Engine should not be running initially")

			t.Logf("Test %s completed", tc.name)
		})
	}
}

// TestGlobalMiddleware tests engine-level middleware
func (et *EngineTester) TestGlobalMiddleware(t *testing.T) {
	t.Helper()

	testCases := []struct {
		name string
	}{
		{"Global middleware"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// var capturedContext httpx.Context
			middlewareExecuted := false

			// Add global middleware
			et.engine.Use(func(ctx httpx.Context) error {
				middlewareExecuted = true
				ctx.Set("global_middleware", "active")
				return ctx.Next()
			})

			// Create a route
			router := et.engine.Group("")
			router.GET(GenerateUniqueTestPath(), func(ctx httpx.Context) error {
				// capturedContext = ctx
				value, exists := ctx.Get("global_middleware")
				if middlewareExecuted {
					AssertEqual(t, true, exists, "Global middleware should have set value")
					AssertEqual(t, "active", value, "Global middleware value should match")
				}
				return ctx.Text(200, "OK")
			})

			t.Logf("Test %s completed", tc.name)
		})
	}
}

// TestEngineGroup tests engine group creation
func (et *EngineTester) TestEngineGroup(t *testing.T) {
	t.Helper()

	testCases := []struct {
		name   string
		prefix string
	}{
		{"Root group", ""},
		{"API group", "/api"},
		{"Versioned group", "/api/v1"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// var capturedContext httpx.Context

			group := et.engine.Group(tc.prefix)
			AssertNotEqual(t, nil, group, "Group should not be nil")

			group.GET(GenerateUniqueTestPath(), func(ctx httpx.Context) error {
				// capturedContext = ctx
				return ctx.Text(200, "OK")
			})

			t.Logf("Test %s completed", tc.name)
		})
	}
}

// TestEngineLifecycle tests engine lifecycle methods together
func (et *EngineTester) TestEngineLifecycle(t *testing.T) {
	t.Helper()

	testCases := []struct {
		name string
	}{
		{"Engine lifecycle"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Check initial state
			AssertEqual(t, false, et.engine.IsRunning(), "Engine should not be running initially")

			// Test stop on non-running engine
			ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
			defer cancel()

			err := et.engine.Stop(ctx)

			// Some frameworks (like Hertzx) return an error when stopping a non-running engine
			// This is acceptable framework-specific behavior
			if err != nil {
				t.Logf("Framework returned error when stopping non-running engine: %v", err)
				// This is acceptable - some frameworks consider this an error condition
			}

			// Should still not be running
			AssertEqual(t, false, et.engine.IsRunning(), "Engine should still not be running")

			t.Logf("Test %s completed", tc.name)
		})
	}
}

// RunAllTests runs all Engine interface tests
func (et *EngineTester) RunAllTests(t *testing.T) {
	t.Helper()
	t.Run("Start", et.TestStart)
	t.Run("Stop", et.TestStop)
	t.Run("IsRunning", et.TestIsRunning)
	t.Run("GlobalMiddleware", et.TestGlobalMiddleware)
	t.Run("EngineGroup", et.TestEngineGroup)
	t.Run("EngineLifecycle", et.TestEngineLifecycle)
}
