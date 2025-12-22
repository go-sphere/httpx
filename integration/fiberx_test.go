package integration

import (
	"testing"

	"github.com/go-sphere/httpx/fiberx"
	httpxtesting "github.com/go-sphere/httpx/testing"
	"github.com/gofiber/fiber/v3"
)

// TestFiberxIntegration demonstrates how to use the httpx testing framework
// with the fiberx adapter. This serves as both a test and an example for
// other developers who want to integrate the testing framework.
func TestFiberxIntegration(t *testing.T) {
	// Create a fiberx engine with test configuration
	engine := fiberx.New(
		fiberx.WithListen(":0"), // Use random port for testing
	)

	// Use common integration tests
	common := NewCommonIntegrationTests(engine, "fiberx")
	common.RunBasicIntegrationTests(t)
}

// TestFiberxTestingFrameworkIntegration demonstrates how to properly integrate
// the testing framework with fiberx for comprehensive testing.
func TestFiberxTestingFrameworkIntegration(t *testing.T) {
	engine := fiberx.New(fiberx.WithListen(":0"))

	// Use common integration tests
	common := NewCommonIntegrationTests(engine, "fiberx")
	common.RunTestingFrameworkIntegrationTests(t)
}

// TestFiberxAbortTracking demonstrates how to test middleware abort behavior
// specifically with the fiberx adapter.
func TestFiberxAbortTracking(t *testing.T) {
	engine := fiberx.New(fiberx.WithListen(":0"))

	// Use common integration tests
	common := NewCommonIntegrationTests(engine, "fiberx")
	common.RunAbortTrackingTests(t)
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

	engine := fiberx.New(
		fiberx.WithEngine(fiberApp),
		fiberx.WithListen(":0"),
	)

	// Test router functionality with fiber-specific features
	t.Run("RouterWithFiberMiddleware", func(t *testing.T) {
		common := NewCommonIntegrationTests(engine, "fiberx")
		common.RunRouterTests(t)
	})

	// Test binding functionality which might have fiber-specific behavior
	t.Run("BinderWithFiberFeatures", func(t *testing.T) {
		common := NewCommonIntegrationTests(engine, "fiberx")
		common.RunBinderTests(t)
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
	engine := fiberx.New(fiberx.WithListen(":0"))

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

	engine := fiberx.New(
		fiberx.WithEngine(fiberApp),
		fiberx.WithListen(":0"),
	)

	// Create test suite with higher concurrency settings
	config := httpxtesting.TestConfig{
		ServerAddr:      ":0",
		RequestTimeout:  httpxtesting.DefaultTestConfig.RequestTimeout,
		ConcurrentUsers: 20, // Higher concurrency for fiber
		TestDataSize:    1024,
	}

	common := NewCommonIntegrationTests(engine, "fiberx")
	suite := common.CreateCustomTestSuite(config)

	// Run concurrency-focused tests
	t.Run("HighConcurrencyTests", func(t *testing.T) {
		suite.RunConcurrencyTests(t)
	})
}

// BenchmarkFiberxPerformance runs performance benchmarks for the fiberx adapter
// using the testing framework's benchmark tools.
func BenchmarkFiberxPerformance(b *testing.B) {
	engine := fiberx.New(fiberx.WithListen(":0"))
	common := NewCommonIntegrationTests(engine, "fiberx")
	common.RunBenchmarks(b)
}

// BenchmarkFiberxVsOthers compares fiberx performance characteristics
// This benchmark can be used to compare against other adapters.
func BenchmarkFiberxVsOthers(b *testing.B) {
	// Create optimized fiber configuration for benchmarking
	fiberApp := fiber.New(fiber.Config{
		// Configure for benchmarking
	})

	engine := fiberx.New(
		fiberx.WithEngine(fiberApp),
		fiberx.WithListen(":0"),
	)

	common := NewCommonIntegrationTests(engine, "fiberx")
	common.RunBenchmarks(b)
}

// TestFiberxBindingIntegration tests fiberx binding functionality with real HTTP requests
func TestFiberxBindingIntegration(t *testing.T) {
	engine := fiberx.New(fiberx.WithListen(":0"))
	common := NewCommonIntegrationTests(engine, "fiberx")
	common.RunBindingIntegrationTests(t)
}

// Example_fiberxIntegration shows how to use the testing framework with fiberx
// in a simple, straightforward way.
func Example_fiberxIntegration() {
	// Create fiberx engine
	engine := fiberx.New(fiberx.WithListen(":8080"))

	// Create test suite using common helper
	common := NewCommonIntegrationTests(engine, "fiberx")
	suite := common.CreateExampleTestSuite()

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
	engine := fiberx.New(
		fiberx.WithEngine(app),
		fiberx.WithListen(":8080"),
	)

	// Create test suite with custom config
	config := httpxtesting.TestConfig{
		ServerAddr:      ":8080",
		RequestTimeout:  httpxtesting.DefaultTestConfig.RequestTimeout,
		ConcurrentUsers: 10,
		TestDataSize:    2048,
	}

	common := NewCommonIntegrationTests(engine, "fiberx")
	suite := common.CreateCustomTestSuite(config)

	// Use the suite in tests
	_ = suite
}
