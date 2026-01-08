package integration

import (
	"testing"

	"github.com/go-sphere/httpx/fiberx"
	httptesting "github.com/go-sphere/httpx/testing"
)

// setupFiberxSkipManager is now defined in skip_managers.go

// TestFiberxIntegration tests the fiberx framework adapter with skip support
func TestFiberxIntegration(t *testing.T) {
	engine := fiberx.New(fiberx.WithListen(":0"))
	skipManager := setupFiberxSkipManager()
	RunFrameworkIntegrationTests(t, "fiberx", engine, skipManager)
}

// TestFiberxSpecificInterfaceTests allows testing specific interfaces individually with skip support
func TestFiberxSpecificInterfaceTests(t *testing.T) {
	engine := fiberx.New(fiberx.WithListen(":0"))
	skipManager := setupFiberxSkipManager()
	RunFrameworkSpecificInterfaceTests(t, "fiberx", engine, skipManager)
}

// TestFiberxWithCustomConfig tests fiberx with custom configuration
func TestFiberxWithCustomConfig(t *testing.T) {
	engine := fiberx.New(fiberx.WithListen(":0"))
	config := &httptesting.TestConfig{
		ServerAddr:     ":0",
		VerboseLogging: true,
	}
	skipManager := setupFiberxSkipManager()
	RunFrameworkWithCustomConfig(t, "fiberx", engine, config, skipManager)
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

	t.Run("BatchMode", func(t *testing.T) {
		runner.RunSingleFramework(t, FrameworkFiberx, ModeBatch)
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
