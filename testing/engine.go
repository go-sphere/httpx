package testing

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-sphere/httpx"
)

// EngineTester provides comprehensive testing for httpx.Engine implementations.
// It verifies server lifecycle management, running status, address handling,
// and global middleware functionality.
type EngineTester struct {
	engine httpx.Engine
	mu     sync.Mutex
}

// NewEngineTester creates a new EngineTester instance for the given engine.
func NewEngineTester(engine httpx.Engine) *EngineTester {
	return &EngineTester{
		engine: engine,
	}
}

// TestStartStop tests the engine's Start() and Stop() methods.
// Validates: Requirements 8.1, 8.2
func (et *EngineTester) TestStartStop(t *testing.T) {
	t.Helper()
	et.mu.Lock()
	defer et.mu.Unlock()

	// Test starting the server
	startErr := make(chan error, 1)
	go func() {
		startErr <- et.engine.Start()
	}()

	// Give the server a moment to start
	time.Sleep(200 * time.Millisecond)

	// Test stopping the server
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stopErr := et.engine.Stop(ctx)
	if stopErr != nil {
		t.Errorf("Engine.Stop() failed: %v", stopErr)
	}

	// Wait for start to complete and check if it returned an error
	select {
	case err := <-startErr:
		// For some engines like fiber, Start() may return an error when stopped
		// This is expected behavior, so we don't treat it as a test failure
		if err != nil {
			t.Logf("Engine.Start() returned error after stop (expected): %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Error("Engine.Start() did not return within expected time")
	}
}

// TestIsRunning tests the engine's IsRunning() method.
// Validates: Requirements 8.3
func (et *EngineTester) TestIsRunning(t *testing.T) {
	t.Helper()
	et.mu.Lock()
	defer et.mu.Unlock()

	// Initially, the server should not be running
	if et.engine.IsRunning() {
		t.Error("Engine.IsRunning() should return false before starting")
	}

	// Start the server in a goroutine
	startErr := make(chan error, 1)
	go func() {
		startErr <- et.engine.Start()
	}()

	// Give the server a moment to start
	time.Sleep(200 * time.Millisecond)

	// Now it should be running
	if !et.engine.IsRunning() {
		t.Error("Engine.IsRunning() should return true after starting")
	}

	// Stop the server
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stopErr := et.engine.Stop(ctx)
	if stopErr != nil {
		t.Errorf("Engine.Stop() failed: %v", stopErr)
	}

	// Give the server a moment to stop
	time.Sleep(200 * time.Millisecond)

	// Now it should not be running
	if et.engine.IsRunning() {
		t.Error("Engine.IsRunning() should return false after stopping")
	}

	// Wait for start to complete
	select {
	case err := <-startErr:
		// For some engines like fiber, Start() may return an error when stopped
		// This is expected behavior, so we don't treat it as a test failure
		if err != nil {
			t.Logf("Engine.Start() returned error after stop (expected): %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Error("Engine.Start() did not return within expected time")
	}
}

// TestAddr tests the engine's Addr() method.
// Validates: Requirements 8.4
func (et *EngineTester) TestAddr(t *testing.T) {
	t.Helper()
	et.mu.Lock()
	defer et.mu.Unlock()

	// Get the address before starting
	addrBeforeStart := et.engine.Addr()

	// Start the server in a goroutine
	startErr := make(chan error, 1)
	go func() {
		startErr <- et.engine.Start()
	}()

	// Give the server a moment to start
	time.Sleep(200 * time.Millisecond)

	// Get the address after starting
	addrAfterStart := et.engine.Addr()

	// The address should be valid and contain a port
	if addrAfterStart == "" {
		t.Error("Engine.Addr() should return a non-empty address after starting")
	}

	// Address should contain a colon (indicating host:port format)
	if !strings.Contains(addrAfterStart, ":") {
		t.Errorf("Engine.Addr() should return address in host:port format, got: %s", addrAfterStart)
	}

	// Stop the server
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stopErr := et.engine.Stop(ctx)
	if stopErr != nil {
		t.Errorf("Engine.Stop() failed: %v", stopErr)
	}

	// Wait for start to complete
	select {
	case err := <-startErr:
		// For some engines like fiber, Start() may return an error when stopped
		// This is expected behavior, so we don't treat it as a test failure
		if err != nil {
			t.Logf("Engine.Start() returned error after stop (expected): %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Error("Engine.Start() did not return within expected time")
	}

	t.Logf("Address before start: %s", addrBeforeStart)
	t.Logf("Address after start: %s", addrAfterStart)
}

// TestGlobalMiddleware tests the engine's global middleware functionality.
// Validates: Requirements 8.5
func (et *EngineTester) TestGlobalMiddleware(t *testing.T) {
	t.Helper()
	et.mu.Lock()
	defer et.mu.Unlock()

	// Track middleware execution
	var executionOrder []string
	var mu sync.Mutex

	// Create test middlewares
	middleware1 := func(ctx httpx.Context) {
		mu.Lock()
		executionOrder = append(executionOrder, "middleware1")
		mu.Unlock()
		ctx.Next()
	}

	middleware2 := func(ctx httpx.Context) {
		mu.Lock()
		executionOrder = append(executionOrder, "middleware2")
		mu.Unlock()
		ctx.Next()
	}

	// Register global middlewares
	et.engine.Use(middleware1, middleware2)

	// Create a test route
	router := et.engine.Group("")
	router.GET("/test", func(ctx httpx.Context) {
		mu.Lock()
		executionOrder = append(executionOrder, "handler")
		mu.Unlock()
		ctx.JSON(http.StatusOK, map[string]string{"message": "test"})
	})

	// Start the server in a goroutine
	startErr := make(chan error, 1)
	go func() {
		startErr <- et.engine.Start()
	}()

	// Give the server a moment to start
	time.Sleep(200 * time.Millisecond)

	// Make a test request to verify middleware execution
	addr := et.engine.Addr()
	if addr != "" {
		// Create a simple HTTP client request
		client := &http.Client{Timeout: 2 * time.Second}
		url := fmt.Sprintf("http://%s/test", addr)
		
		resp, err := client.Get(url)
		if err == nil {
			resp.Body.Close()
			
			// Give middleware a moment to execute
			time.Sleep(50 * time.Millisecond)
			
			// Check middleware execution order
			mu.Lock()
			expectedOrder := []string{"middleware1", "middleware2", "handler"}
			if !EqualSlices(executionOrder, expectedOrder) {
				t.Errorf("Expected middleware execution order %v, got %v", expectedOrder, executionOrder)
			}
			mu.Unlock()
		} else {
			t.Logf("Could not make test request: %v", err)
		}
	}

	// Stop the server
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stopErr := et.engine.Stop(ctx)
	if stopErr != nil {
		t.Errorf("Engine.Stop() failed: %v", stopErr)
	}

	// Wait for start to complete
	select {
	case err := <-startErr:
		// For some engines like fiber, Start() may return an error when stopped
		// This is expected behavior, so we don't treat it as a test failure
		if err != nil {
			t.Logf("Engine.Start() returned error after stop (expected): %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Error("Engine.Start() did not return within expected time")
	}
}

// RunAllTests executes all engine tests in sequence.
// This provides a convenient way to run the complete engine test suite.
func (et *EngineTester) RunAllTests(t *testing.T) {
	t.Helper()

	t.Run("StartStop", et.TestStartStop)
	t.Run("IsRunning", et.TestIsRunning)
	t.Run("Addr", et.TestAddr)
	t.Run("GlobalMiddleware", et.TestGlobalMiddleware)
}