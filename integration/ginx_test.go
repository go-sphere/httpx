package integration

import (
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/go-sphere/httpx/ginx"
)

// TestGinxIntegration demonstrates how to use the httpx testing framework
// with the ginx adapter. This serves as both a test and an example for
// other developers who want to integrate the testing framework.
func TestGinxIntegration(t *testing.T) {
	// Set gin to test mode to reduce noise in test output
	gin.SetMode(gin.TestMode)

	// Create a ginx engine with test configuration
	engine := ginx.New(
		ginx.WithServerAddr(":0"), // Use random port for testing
	)

	// Use common integration tests
	common := NewCommonIntegrationTests(engine, "ginx")
	common.RunBasicIntegrationTests(t)
}

// TestGinxTestingFrameworkIntegration demonstrates how to properly integrate
// the testing framework with ginx for comprehensive testing.
func TestGinxTestingFrameworkIntegration(t *testing.T) {
	gin.SetMode(gin.TestMode)

	engine := ginx.New(ginx.WithServerAddr(":0"))

	// Use common integration tests
	common := NewCommonIntegrationTests(engine, "ginx")
	common.RunTestingFrameworkIntegrationTests(t)
}

// TestGinxAbortTracking demonstrates how to test middleware abort behavior
// specifically with the ginx adapter.
func TestGinxAbortTracking(t *testing.T) {
	gin.SetMode(gin.TestMode)

	engine := ginx.New(ginx.WithServerAddr(":0"))

	// Use common integration tests
	common := NewCommonIntegrationTests(engine, "ginx")
	common.RunAbortTrackingTests(t)
}

// TestGinxBindingIntegration tests ginx binding functionality with real HTTP requests
func TestGinxBindingIntegration(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// Test HTTP-based binding
	t.Run("HTTPBindingTests", func(t *testing.T) {
		// Create a fresh engine for HTTP tests
		engine := ginx.New(ginx.WithServerAddr(":0"))
		common := NewCommonIntegrationTests(engine, "ginx")
		common.RunBindingIntegrationTests(t)
	})
}

// BenchmarkGinxPerformance runs performance benchmarks for the ginx adapter
// using the testing framework's benchmark tools.
func BenchmarkGinxPerformance(b *testing.B) {
	gin.SetMode(gin.TestMode)

	engine := ginx.New(ginx.WithServerAddr(":0"))
	common := NewCommonIntegrationTests(engine, "ginx")
	common.RunBenchmarks(b)
}

// Example_ginxIntegration shows how to use the testing framework with ginx
// in a simple, straightforward way.
func Example_ginxIntegration() {
	// Set gin to test mode
	gin.SetMode(gin.TestMode)

	// Create ginx engine
	engine := ginx.New(ginx.WithServerAddr(":8080"))

	// Create test suite using common helper
	common := NewCommonIntegrationTests(engine, "ginx")
	suite := common.CreateExampleTestSuite()

	// In a real test, you would call:
	// suite.RunAllTests(t)

	// This example demonstrates the basic setup
	_ = suite
}