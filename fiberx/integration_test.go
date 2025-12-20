package fiberx

import (
	"testing"

	"github.com/go-sphere/httpx"
	httpxtesting "github.com/go-sphere/httpx/testing"
	"github.com/gofiber/fiber/v3"
)

// TestFiberxIntegration demonstrates how to use the httpx testing framework
// with the fiberx adapter. This serves as both a test and an example for
// other developers who want to integrate the testing framework.
func TestFiberxIntegration(t *testing.T) {
	// Create a fiberx engine with test configuration
	engine := New(
		WithListen(":0"), // Use random port for testing
	)
	
	// Test basic engine functionality
	t.Run("EngineBasics", func(t *testing.T) {
		// Test that engine is not running initially
		if engine.IsRunning() {
			t.Error("Expected engine to not be running initially")
		}
		
		// Test that we can get the address
		addr := engine.Addr()
		if addr != ":0" {
			t.Errorf("Expected address :0, got %s", addr)
		}
	})
	
	// Test router functionality
	t.Run("RouterFunctionality", func(t *testing.T) {
		// Test basic route registration
		router := engine.Group("/api")
		
		// Test that we can register routes without errors
		router.GET("/test", func(ctx httpx.Context) {
			ctx.Text(200, "OK")
		})
		
		router.POST("/data", func(ctx httpx.Context) {
			ctx.JSON(200, map[string]string{"status": "ok"})
		})
		
		// Test middleware registration
		router.Use(func(ctx httpx.Context) {
			ctx.Set("middleware", "executed")
			ctx.Next()
		})
		
		t.Log("Successfully registered routes and middleware")
	})
	
	// Test abort tracker functionality
	t.Run("AbortTracking", func(t *testing.T) {
		tracker := httpxtesting.NewAbortTracker()
		
		// Test initial state
		if len(tracker.Steps) != 0 {
			t.Errorf("Expected empty steps initially, got %d", len(tracker.Steps))
		}
		
		if len(tracker.AbortedStates) != 0 {
			t.Errorf("Expected empty aborted states initially, got %d", len(tracker.AbortedStates))
		}
		
		// Test reset functionality
		tracker.Steps = append(tracker.Steps, "test")
		tracker.AbortedStates = append(tracker.AbortedStates, false)
		
		tracker.Reset()
		
		if len(tracker.Steps) != 0 {
			t.Errorf("Expected empty steps after reset, got %d", len(tracker.Steps))
		}
		
		if len(tracker.AbortedStates) != 0 {
			t.Errorf("Expected empty aborted states after reset, got %d", len(tracker.AbortedStates))
		}
	})
}

// TestFiberxTestingFrameworkIntegration demonstrates how to properly integrate
// the testing framework with fiberx for comprehensive testing.
func TestFiberxTestingFrameworkIntegration(t *testing.T) {
	engine := New(WithListen(":0"))
	
	// Test individual testing components
	t.Run("AbortTrackerIntegration", func(t *testing.T) {
		tracker := httpxtesting.NewAbortTracker()
		
		// Test that we can set up abort testing
		httpxtesting.SetupAbortEngine(engine, tracker)
		
		// Verify tracker is properly initialized
		if len(tracker.Steps) != 0 {
			t.Error("Expected empty steps after setup")
		}
		
		if len(tracker.AbortedStates) != 0 {
			t.Error("Expected empty aborted states after setup")
		}
		
		t.Log("AbortTracker integration successful")
	})
	
	t.Run("TestingToolsCreation", func(t *testing.T) {
		// Test that we can create all testing tools without errors
		requestTester := httpxtesting.NewRequestTester(engine)
		if requestTester == nil {
			t.Error("Failed to create RequestTester")
		}
		
		binderTester := httpxtesting.NewBinderTester(engine)
		if binderTester == nil {
			t.Error("Failed to create BinderTester")
		}
		
		responderTester := httpxtesting.NewResponderTester(engine)
		if responderTester == nil {
			t.Error("Failed to create ResponderTester")
		}
		
		stateStoreTester := httpxtesting.NewStateStoreTester(engine)
		if stateStoreTester == nil {
			t.Error("Failed to create StateStoreTester")
		}
		
		routerTester := httpxtesting.NewRouterTester(engine)
		if routerTester == nil {
			t.Error("Failed to create RouterTester")
		}
		
		engineTester := httpxtesting.NewEngineTester(engine)
		if engineTester == nil {
			t.Error("Failed to create EngineTester")
		}
		
		t.Log("All testing tools created successfully")
	})
	
	t.Run("TestSuiteCreation", func(t *testing.T) {
		// Test that we can create test suites
		suite := httpxtesting.NewTestSuite("fiberx-test", engine)
		if suite == nil {
			t.Error("Failed to create TestSuite")
		}
		
		// Test with custom config
		config := httpxtesting.TestConfig{
			ServerAddr:      ":0",
			RequestTimeout:  httpxtesting.DefaultTestConfig.RequestTimeout,
			ConcurrentUsers: 3,
			TestDataSize:    256,
		}
		
		customSuite := httpxtesting.NewTestSuiteWithConfig("fiberx-custom", engine, config)
		if customSuite == nil {
			t.Error("Failed to create TestSuite with custom config")
		}
		
		t.Log("Test suites created successfully")
	})
}

// TestFiberxAbortTracking demonstrates how to test middleware abort behavior
// specifically with the fiberx adapter.
func TestFiberxAbortTracking(t *testing.T) {
	engine := New(WithListen(":0"))
	
	// Create abort tracker for testing middleware behavior
	tracker := httpxtesting.NewAbortTracker()
	
	// Set up the engine with abort testing middleware
	httpxtesting.SetupAbortEngine(engine, tracker)
	
	// Test abort tracking functionality
	t.Run("AbortTrackerInitialization", func(t *testing.T) {
		if len(tracker.Steps) != 0 {
			t.Errorf("Expected empty steps on initialization, got %d", len(tracker.Steps))
		}
		if len(tracker.AbortedStates) != 0 {
			t.Errorf("Expected empty aborted states on initialization, got %d", len(tracker.AbortedStates))
		}
	})
	
	t.Run("AbortTrackerReset", func(t *testing.T) {
		// Add some test data
		tracker.Steps = append(tracker.Steps, "test_step")
		tracker.AbortedStates = append(tracker.AbortedStates, false)
		
		// Reset and verify
		tracker.Reset()
		if len(tracker.Steps) != 0 {
			t.Errorf("Expected empty steps after reset, got %d", len(tracker.Steps))
		}
		if len(tracker.AbortedStates) != 0 {
			t.Errorf("Expected empty aborted states after reset, got %d", len(tracker.AbortedStates))
		}
	})
}

// TestFiberxSpecificFeatures tests fiberx-specific features and behaviors
// that might differ from other adapters.
func TestFiberxSpecificFeatures(t *testing.T) {
	// Test with fiber's built-in middleware
	fiberApp := fiber.New(fiber.Config{
		// Configure for testing
	})
	
	// Add fiber-specific middleware
	fiberApp.Use(func(c fiber.Ctx) error {
		// Custom middleware for testing
		c.Set("X-Custom-Header", "test-value")
		return c.Next()
	})
	
	engine := New(
		WithEngine(fiberApp),
		WithListen(":0"),
	)
	
	// Test router functionality with fiber-specific features
	t.Run("RouterWithFiberMiddleware", func(t *testing.T) {
		routerTester := httpxtesting.NewRouterTester(engine)
		routerTester.RunAllTests(t)
	})
	
	// Test binding functionality which might have fiber-specific behavior
	t.Run("BinderWithFiberFeatures", func(t *testing.T) {
		binderTester := httpxtesting.NewBinderTester(engine)
		binderTester.RunAllTests(t)
	})
	
	// Test fiber's fast HTTP features
	t.Run("FiberFastHTTPFeatures", func(t *testing.T) {
		// Test features specific to fiber's fasthttp backend
		requestTester := httpxtesting.NewRequestTester(engine)
		requestTester.RunAllTests(t)
	})
}

// TestFiberxEngineLifecycle tests the engine start/stop lifecycle
// which might behave differently in fiber compared to other adapters.
func TestFiberxEngineLifecycle(t *testing.T) {
	engine := New(WithListen(":0"))
	
	// Test engine lifecycle management
	t.Run("EngineLifecycle", func(t *testing.T) {
		engineTester := httpxtesting.NewEngineTester(engine)
		engineTester.RunAllTests(t)
	})
}

// TestFiberxConcurrentRequests specifically tests fiber's ability to handle
// concurrent requests, which is one of its key performance features.
func TestFiberxConcurrentRequests(t *testing.T) {
	// Create engine optimized for concurrency
	fiberApp := fiber.New(fiber.Config{
		// Configure for testing
	})
	
	engine := New(
		WithEngine(fiberApp),
		WithListen(":0"),
	)
	
	// Create test suite with higher concurrency settings
	config := httpxtesting.TestConfig{
		ServerAddr:      ":0",
		RequestTimeout:  httpxtesting.DefaultTestConfig.RequestTimeout,
		ConcurrentUsers: 20, // Higher concurrency for fiber
		TestDataSize:    1024,
	}
	
	suite := httpxtesting.NewTestSuiteWithConfig("fiberx-concurrent", engine, config)
	_ = suite // Use suite to avoid unused variable error
	
	// Run concurrency-focused tests
	t.Run("HighConcurrencyTests", func(t *testing.T) {
		suite.RunConcurrencyTests(t)
	})
}

// BenchmarkFiberxPerformance runs performance benchmarks for the fiberx adapter
// using the testing framework's benchmark tools.
func BenchmarkFiberxPerformance(b *testing.B) {
	engine := New(WithListen(":0"))
	suite := httpxtesting.NewTestSuite("fiberx-benchmark", engine)
	
	// Run all performance benchmarks
	suite.RunBenchmarks(b)
}

// BenchmarkFiberxVsOthers compares fiberx performance characteristics
// This benchmark can be used to compare against other adapters.
func BenchmarkFiberxVsOthers(b *testing.B) {
	// Create optimized fiber configuration for benchmarking
	fiberApp := fiber.New(fiber.Config{
		// Configure for benchmarking
	})
	
	engine := New(
		WithEngine(fiberApp),
		WithListen(":0"),
	)
	
	suite := httpxtesting.NewTestSuite("fiberx-optimized", engine)
	
	// Run performance-focused benchmarks
	suite.RunBenchmarks(b)
}

// Example_fiberxIntegration shows how to use the testing framework with fiberx
// in a simple, straightforward way.
func Example_fiberxIntegration() {
	// Create fiberx engine
	engine := New(WithListen(":8080"))
	
	// Create test suite
	suite := httpxtesting.NewTestSuite("fiberx-example", engine)
	
	// In a real test, you would call:
	// suite.RunAllTests(t)
	
	// This example demonstrates the basic setup
	_ = suite
}

// Example_fiberxCustomConfiguration shows how to create a test suite
// with custom fiber configuration for specific testing needs.
func Example_fiberxCustomConfiguration() {
	// Create custom fiber app
	app := fiber.New(fiber.Config{
		// Configure for testing
	})
	
	// Create fiberx engine with custom app
	engine := New(
		WithEngine(app),
		WithListen(":8080"),
	)
	
	// Create test suite with custom config
	config := httpxtesting.TestConfig{
		ServerAddr:      ":8080",
		RequestTimeout:  httpxtesting.DefaultTestConfig.RequestTimeout,
		ConcurrentUsers: 10,
		TestDataSize:    2048,
	}
	
	suite := httpxtesting.NewTestSuiteWithConfig("fiberx-custom", engine, config)
	
	// Use the suite in tests
	_ = suite
}