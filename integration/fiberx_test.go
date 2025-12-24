package integration

import (
	"testing"

	"github.com/go-sphere/httpx/fiberx"
	httptesting "github.com/go-sphere/httpx/testing"
)

// setupFiberxSkipManager is now defined in skip_managers.go

// TestFiberxIntegration tests the fiberx framework adapter with skip support
func TestFiberxIntegration(t *testing.T) {
	// Create fiberx engine with test configuration
	engine := fiberx.New(fiberx.WithListen(":0"))

	// Create common integration tests instance
	tc := NewTestCases("fiberx", engine)

	// Set up skip manager for known failing tests
	skipManager := setupFiberxSkipManager()

	// Validate framework integration first
	t.Run("ValidateIntegration", func(t *testing.T) {
		tc.ValidateFrameworkIntegration(t)
	})

	// Run all interface tests with skip support
	t.Run("AllInterfaceTests", func(t *testing.T) {
		tc.RunAllInterfaceTests(t)
	})

	// Run individual interface tests with skip support for better isolation
	t.Run("IndividualInterfaceTestsWithSkipSupport", func(t *testing.T) {
		tc.RunIndividualInterfaceTestsWithSkipSupport(t, skipManager)
	})
}

// TestFiberxSpecificInterfaceTests allows testing specific interfaces individually with skip support
func TestFiberxSpecificInterfaceTests(t *testing.T) {
	engine := fiberx.New(fiberx.WithListen(":0"))
	tc := NewTestCases("fiberx", engine)
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
		"Router",
		"Engine",
	}

	for _, interfaceName := range testCases {
		t.Run(interfaceName, func(t *testing.T) {
			tc.RunWithSkipSupport(t, skipManager, interfaceName, func(t *testing.T) {
				tc.RunSpecificInterfaceTest(t, interfaceName)
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

	tc := NewTestCasesWithConfig("fiberx", engine, config)
	skipManager := setupFiberxSkipManager()

	t.Run("CustomConfigTests", func(t *testing.T) {
		// Run tests with skip support
		tc.RunWithSkipSupport(t, skipManager, "all", func(t *testing.T) {
			tc.RunAllInterfaceTests(t)
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
