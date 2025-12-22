package integration

import (
	"testing"

	"github.com/go-sphere/httpx/echox"
	"github.com/labstack/echo/v4"
)

// TestEchoxIntegration demonstrates how to use the httpx testing framework
// with the echox adapter. This serves as both a test and an example for
// other developers who want to integrate the testing framework.
func TestEchoxIntegration(t *testing.T) {
	// Create an echox engine with test configuration
	engine := echox.New(
		echox.WithServerAddr(":0"), // Use random port for testing
	)

	// Use common integration tests
	common := NewCommonIntegrationTests(engine, "echox")
	common.RunBasicIntegrationTests(t)
}

// TestEchoxTestingFrameworkIntegration demonstrates how to properly integrate
// the testing framework with echox for comprehensive testing.
func TestEchoxTestingFrameworkIntegration(t *testing.T) {
	engine := echox.New(echox.WithServerAddr(":0"))

	// Use common integration tests
	common := NewCommonIntegrationTests(engine, "echox")
	common.RunTestingFrameworkIntegrationTests(t)
}

// TestEchoxAbortTracking demonstrates how to test middleware abort behavior
// specifically with the echox adapter.
func TestEchoxAbortTracking(t *testing.T) {
	engine := echox.New(echox.WithServerAddr(":0"))

	// Use common integration tests
	common := NewCommonIntegrationTests(engine, "echox")
	common.RunAbortTrackingTests(t)
}

// TestEchoxSpecificFeatures tests echox-specific features and behaviors
// that might differ from other adapters.
func TestEchoxSpecificFeatures(t *testing.T) {
	// Test with echo's built-in middleware
	echoEngine := echo.New()

	engine := echox.New(
		echox.WithEngine(echoEngine),
		echox.WithServerAddr(":0"),
	)

	// Test router functionality with echo-specific features
	t.Run("RouterWithEchoMiddleware", func(t *testing.T) {
		common := NewCommonIntegrationTests(engine, "echox")
		common.RunRouterTests(t)
	})

	// Test binding functionality which might have echo-specific behavior
	t.Run("BinderWithEchoFeatures", func(t *testing.T) {
		common := NewCommonIntegrationTests(engine, "echox")
		common.RunBinderTests(t)
	})
}

// BenchmarkEchoxPerformance runs performance benchmarks for the echox adapter
// using the testing framework's benchmark tools.
func BenchmarkEchoxPerformance(b *testing.B) {
	engine := echox.New(echox.WithServerAddr(":0"))
	common := NewCommonIntegrationTests(engine, "echox")
	common.RunBenchmarks(b)
}

// TestEchoxBindingIntegration tests echox binding functionality with real HTTP requests
func TestEchoxBindingIntegration(t *testing.T) {
	engine := echox.New(echox.WithServerAddr(":0"))
	common := NewCommonIntegrationTests(engine, "echox")
	common.RunBindingIntegrationTests(t)
}

// Example_echoxIntegration shows how to use the testing framework with echox
// in a simple, straightforward way.
func Example_echoxIntegration() {
	// Create echox engine
	engine := echox.New(echox.WithServerAddr(":8080"))

	// Create test suite using common helper
	common := NewCommonIntegrationTests(engine, "echox")
	suite := common.CreateExampleTestSuite()

	// In a real test, you would call:
	// suite.RunAllTests(t)

	// This example demonstrates the basic setup
	_ = suite
}
