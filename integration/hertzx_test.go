package integration

import (
	"testing"

	"github.com/go-sphere/httpx/hertzx"
	httptesting "github.com/go-sphere/httpx/testing"
)

// setupHertzxSkipManager configures known failing tests for hertzx
func setupHertzxSkipManager() *TestSkipManager {
	skipManager := NewTestSkipManager()

	// Add known failing tests for hertzx - these should be updated as issues are fixed

	// Currently no tests need to be skipped - framework-specific behaviors are handled in the tests

	// Uncomment and adjust these as needed based on actual test failures:
	// skipManager.AddSkippedTest("hertzx", "RequestInfo", "Headers", "Header handling differences")
	// skipManager.AddSkippedTest("hertzx", "Responder", "JSON", "JSON response differences")
	// skipManager.AddSkippedTest("hertzx", "Router", "Static", "Static file serving differences")

	return skipManager
}

// TestHertzxIntegration tests the hertzx framework adapter with skip support
func TestHertzxIntegration(t *testing.T) {
	// Create hertzx engine with test configuration
	engine := hertzx.New()

	// Create common integration tests instance
	cit := NewCommonIntegrationTests("hertzx", engine)

	// Set up skip manager for known failing tests
	skipManager := setupHertzxSkipManager()

	// Validate framework integration first
	t.Run("ValidateIntegration", func(t *testing.T) {
		cit.ValidateFrameworkIntegration(t)
	})

	// Run all interface tests with skip support
	t.Run("AllInterfaceTests", func(t *testing.T) {
		cit.RunAllInterfaceTests(t)
	})

	// Run individual interface tests with skip support for better isolation
	t.Run("IndividualInterfaceTestsWithSkipSupport", func(t *testing.T) {
		cit.RunIndividualInterfaceTestsWithSkipSupport(t, skipManager)
	})
}

// TestHertzxSpecificInterfaceTests allows testing specific interfaces individually with skip support
func TestHertzxSpecificInterfaceTests(t *testing.T) {
	engine := hertzx.New()
	cit := NewCommonIntegrationTests("hertzx", engine)
	skipManager := setupHertzxSkipManager()

	// Test each interface individually with skip support
	testCases := []string{
		"RequestInfo",
		"Request",
		"BodyAccess",
		"FormAccess",
		"Binder",
		"Responder",
		"StateStore",
		"Router",
		"Engine",
	}

	for _, interfaceName := range testCases {
		t.Run(interfaceName, func(t *testing.T) {
			cit.RunWithSkipSupport(t, skipManager, interfaceName, func(t *testing.T) {
				cit.RunSpecificInterfaceTest(t, interfaceName)
			})
		})
	}
}

// TestHertzxWithCustomConfig tests hertzx with custom configuration
func TestHertzxWithCustomConfig(t *testing.T) {
	engine := hertzx.New()

	// Create custom test configuration
	config := &httptesting.TestConfig{
		ServerAddr:     ":0",
		VerboseLogging: true,
	}

	cit := NewCommonIntegrationTestsWithConfig("hertzx", engine, config)
	skipManager := setupHertzxSkipManager()

	t.Run("CustomConfigTests", func(t *testing.T) {
		// Run tests with skip support
		cit.RunWithSkipSupport(t, skipManager, "all", func(t *testing.T) {
			cit.RunAllInterfaceTests(t)
		})
	})
}

// BenchmarkHertzxIntegration provides performance benchmarks for hertzx
func BenchmarkHertzxIntegration(b *testing.B) {
	engine := hertzx.New()
	cit := NewCommonIntegrationTests("hertzx", engine)

	// Benchmark interface tests
	cit.BenchmarkInterfaceTests(b)
}

// TestHertzxSkipManagerConfiguration tests the skip manager configuration
func TestHertzxSkipManagerConfiguration(t *testing.T) {
	skipManager := setupHertzxSkipManager()

	// Test that skip manager is properly configured
	skippedTests := skipManager.GetSkippedTests("hertzx")
	t.Logf("Hertzx has %d configured skipped tests", len(skippedTests))

	// Log skipped tests for visibility
	for _, test := range skippedTests {
		t.Logf("Skipped test: %s.%s - %s", test.Interface, test.Method, test.Reason)
	}
}

// TestHertzxEngineLifecycle tests the engine start/stop lifecycle
func TestHertzxEngineLifecycle(t *testing.T) {
	engine := hertzx.New()

	// Test initial state
	if engine.IsRunning() {
		t.Error("Engine should not be running initially")
	}

	// Note: We don't actually start/stop the engine in tests to avoid port conflicts
	// The actual lifecycle testing is handled by the Engine interface tester
	t.Log("Engine lifecycle testing delegated to Engine interface tester")
}
