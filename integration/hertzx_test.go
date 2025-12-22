package integration

import (
	"testing"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/go-sphere/httpx/hertzx"
)

// TestHertzxIntegration demonstrates how to use the httpx testing framework
// with the hertzx adapter. This serves as both a test and an example for
// other developers who want to integrate the testing framework.
func TestHertzxIntegration(t *testing.T) {
	// Create a hertzx engine with test configuration
	engine := hertzx.New()

	// Use common integration tests
	common := NewCommonIntegrationTests(engine, "hertzx")
	common.RunBasicIntegrationTests(t)
}

// TestHertzxTestingFrameworkIntegration demonstrates how to properly integrate
// the testing framework with hertzx for comprehensive testing.
func TestHertzxTestingFrameworkIntegration(t *testing.T) {
	engine := hertzx.New()

	// Use common integration tests
	common := NewCommonIntegrationTests(engine, "hertzx")
	common.RunTestingFrameworkIntegrationTests(t)
}

// TestHertzxAbortTracking demonstrates how to test middleware abort behavior
// specifically with the hertzx adapter.
func TestHertzxAbortTracking(t *testing.T) {
	engine := hertzx.New()

	// Use common integration tests
	common := NewCommonIntegrationTests(engine, "hertzx")
	common.RunAbortTrackingTests(t)
}

// TestHertzxSpecificFeatures tests hertzx-specific features and behaviors
// that might differ from other adapters. Note that hertzx is a minimal-level
// adaptation, so some features may have limitations.
func TestHertzxSpecificFeatures(t *testing.T) {
	// Test with hertz's built-in configuration
	hertzEngine := server.Default()

	engine := hertzx.New(
		hertzx.WithEngine(hertzEngine),
	)

	// Test router functionality with hertz-specific features
	t.Run("RouterWithHertzFeatures", func(t *testing.T) {
		common := NewCommonIntegrationTests(engine, "hertzx")
		common.RunRouterTests(t)
	})

	// Test binding functionality - hertzx may have limitations
	t.Run("BinderWithHertzFeatures", func(t *testing.T) {
		common := NewCommonIntegrationTests(engine, "hertzx")
		common.RunBinderTests(t)
	})
}

// BenchmarkHertzxPerformance runs performance benchmarks for the hertzx adapter
// using the testing framework's benchmark tools.
func BenchmarkHertzxPerformance(b *testing.B) {
	engine := hertzx.New()
	common := NewCommonIntegrationTests(engine, "hertzx")
	common.RunBenchmarks(b)
}

// TestHertzxBindingIntegration tests hertzx binding functionality with real HTTP requests
func TestHertzxBindingIntegration(t *testing.T) {
	engine := hertzx.New()
	common := NewCommonIntegrationTests(engine, "hertzx")
	common.RunBindingIntegrationTests(t)
}

// Example_hertzxIntegration shows how to use the testing framework with hertzx
// in a simple, straightforward way.
func Example_hertzxIntegration() {
	// Create hertzx engine
	engine := hertzx.New()

	// Create test suite using common helper
	common := NewCommonIntegrationTests(engine, "hertzx")
	suite := common.CreateExampleTestSuite()

	// In a real test, you would call:
	// suite.RunAllTests(t)

	// This example demonstrates the basic setup
	_ = suite
}
