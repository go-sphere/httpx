package integration

import (
	"testing"

	"github.com/go-sphere/httpx/hertzx"
	httptesting "github.com/go-sphere/httpx/testing"
)

// setupHertzxSkipManager is now defined in skip_managers.go

// TestHertzxIntegration tests the hertzx framework adapter with skip support
func TestHertzxIntegration(t *testing.T) {
	engine := hertzx.New()
	skipManager := setupHertzxSkipManager()
	RunFrameworkIntegrationTests(t, "hertzx", engine, skipManager)
}

// TestHertzxSpecificInterfaceTests allows testing specific interfaces individually with skip support
func TestHertzxSpecificInterfaceTests(t *testing.T) {
	engine := hertzx.New()
	skipManager := setupHertzxSkipManager()
	RunFrameworkSpecificInterfaceTests(t, "hertzx", engine, skipManager)
}

// TestHertzxWithCustomConfig tests hertzx with custom configuration
func TestHertzxWithCustomConfig(t *testing.T) {
	engine := hertzx.New()
	config := &httptesting.TestConfig{
		ServerAddr:     ":0",
		VerboseLogging: true,
	}
	skipManager := setupHertzxSkipManager()
	RunFrameworkWithCustomConfig(t, "hertzx", engine, config, skipManager)
}

// BenchmarkHertzxIntegration provides performance benchmarks for hertzx
func BenchmarkHertzxIntegration(b *testing.B) {
	engine := hertzx.New()
	tc := NewTestCases("hertzx", engine)

	// Benchmark interface tests
	tc.BenchmarkInterfaceTests(b)
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
