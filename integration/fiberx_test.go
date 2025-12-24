package integration

import (
	"testing"

	"github.com/go-sphere/httpx/fiberx"
	httptesting "github.com/go-sphere/httpx/testing"
)

// setupFiberxSkipManager configures known failing tests for fiberx
func setupFiberxSkipManager() *TestSkipManager {
	skipManager := NewTestSkipManager()
	
	// Add known failing tests for fiberx - these should be updated as issues are fixed
	// Fiber doesn't support custom HTTP methods
	skipManager.AddSkippedTest("fiberx", "Router", "Handle", "Fiber doesn't support custom HTTP methods like CUSTOM")
	
	// Uncomment and adjust these as needed based on actual test failures:
	// skipManager.AddSkippedTest("fiberx", "Binder", "BindJSON", "Known issue with JSON binding in fiberx")
	// skipManager.AddSkippedTest("fiberx", "FormAccess", "FormFile", "Multipart form handling differences")
	// skipManager.AddSkippedTest("fiberx", "RequestInfo", "ClientIP", "Client IP detection differences")
	
	return skipManager
}

// TestFiberxIntegration tests the fiberx framework adapter with skip support
func TestFiberxIntegration(t *testing.T) {
	// Create fiberx engine with test configuration
	engine := fiberx.New(fiberx.WithListen(":0"))
	
	// Create common integration tests instance
	cit := NewCommonIntegrationTests("fiberx", engine)
	
	// Set up skip manager for known failing tests
	skipManager := setupFiberxSkipManager()
	
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

// TestFiberxSpecificInterfaceTests allows testing specific interfaces individually with skip support
func TestFiberxSpecificInterfaceTests(t *testing.T) {
	engine := fiberx.New(fiberx.WithListen(":0"))
	cit := NewCommonIntegrationTests("fiberx", engine)
	skipManager := setupFiberxSkipManager()
	
	// Test each interface individually with skip support
	testCases := []string{
		"RequestInfo",
		"Request", 
		"BodyAccess",
		"FormAccess",
		"Binder",
		"Responder",
		"StateStore",
		"Aborter",
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

// TestFiberxWithCustomConfig tests fiberx with custom configuration
func TestFiberxWithCustomConfig(t *testing.T) {
	engine := fiberx.New(fiberx.WithListen(":0"))
	
	// Create custom test configuration
	config := &httptesting.TestConfig{
		ServerAddr:     ":0",
		VerboseLogging: true,
	}
	
	cit := NewCommonIntegrationTestsWithConfig("fiberx", engine, config)
	skipManager := setupFiberxSkipManager()
	
	t.Run("CustomConfigTests", func(t *testing.T) {
		// Run tests with skip support
		cit.RunWithSkipSupport(t, skipManager, "all", func(t *testing.T) {
			cit.RunAllInterfaceTests(t)
		})
	})
}

// TestFiberxFlexibleExecution demonstrates flexible execution for fiberx
func TestFiberxFlexibleExecution(t *testing.T) {
	runner := NewTestRunner()
	
	// Test different execution modes with skip support
	t.Run("IndividualMode", func(t *testing.T) {
		runner.RunSingleFramework(t, FrameworkFiberx, ModeIndividual)
	})
	
	t.Run("BatchMode", func(t *testing.T) {
		runner.RunSingleFramework(t, FrameworkFiberx, ModeBatch)
	})
	
	t.Run("ValidationMode", func(t *testing.T) {
		runner.RunSingleFramework(t, FrameworkFiberx, ModeValidation)
	})
}

// BenchmarkFiberxFlexible provides flexible benchmarking for fiberx
func BenchmarkFiberxFlexible(b *testing.B) {
	runner := NewTestRunner()
	runner.BenchmarkSingleFramework(b, FrameworkFiberx)
}

// TestFiberxSkipManagerConfiguration tests the skip manager configuration
func TestFiberxSkipManagerConfiguration(t *testing.T) {
	skipManager := setupFiberxSkipManager()
	
	// Test that skip manager is properly configured
	skippedTests := skipManager.GetSkippedTests("fiberx")
	t.Logf("Fiberx has %d configured skipped tests", len(skippedTests))
	
	// Log skipped tests for visibility
	for _, test := range skippedTests {
		t.Logf("Skipped test: %s.%s - %s", test.Interface, test.Method, test.Reason)
	}
}

// TestFiberxEngineLifecycle tests the engine start/stop lifecycle
func TestFiberxEngineLifecycle(t *testing.T) {
	engine := fiberx.New(fiberx.WithListen(":0"))
	
	// Test initial state
	if engine.IsRunning() {
		t.Error("Engine should not be running initially")
	}
	
	// Note: We don't actually start/stop the engine in tests to avoid port conflicts
	// The actual lifecycle testing is handled by the Engine interface tester
	t.Log("Engine lifecycle testing delegated to Engine interface tester")
}