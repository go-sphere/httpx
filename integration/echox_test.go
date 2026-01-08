package integration

import (
	"testing"

	"github.com/go-sphere/httpx/echox"
	httptesting "github.com/go-sphere/httpx/testing"
)

// setupEchoxSkipManager is now defined in skip_managers.go

// TestEchoxIntegration tests the echox framework adapter with skip support
func TestEchoxIntegration(t *testing.T) {
	engine := echox.New(echox.WithServerAddr(":0"))
	skipManager := setupEchoxSkipManager()
	RunFrameworkIntegrationTests(t, "echox", engine, skipManager)
}

// TestEchoxSpecificInterfaceTests allows testing specific interfaces individually with skip support
func TestEchoxSpecificInterfaceTests(t *testing.T) {
	engine := echox.New(echox.WithServerAddr(":0"))
	skipManager := setupEchoxSkipManager()
	RunFrameworkSpecificInterfaceTests(t, "echox", engine, skipManager)
}

// TestEchoxWithCustomConfig tests echox with custom configuration
func TestEchoxWithCustomConfig(t *testing.T) {
	engine := echox.New(echox.WithServerAddr(":0"))
	config := &httptesting.TestConfig{
		ServerAddr:     ":0",
		VerboseLogging: true,
	}
	skipManager := setupEchoxSkipManager()
	RunFrameworkWithCustomConfig(t, "echox", engine, config, skipManager)
}

// BenchmarkEchoxIntegration provides performance benchmarks for echox
func BenchmarkEchoxIntegration(b *testing.B) {
	engine := echox.New(echox.WithServerAddr(":0"))
	tc := NewTestCases("echox", engine)

	// Benchmark interface tests
	tc.BenchmarkInterfaceTests(b)
}

// TestEchoxSkipManagerConfiguration tests the skip manager configuration
func TestEchoxSkipManagerConfiguration(t *testing.T) {
	skipManager := setupEchoxSkipManager()

	// Test that skip manager is properly configured
	skippedTests := skipManager.GetSkippedTests("echox")
	t.Logf("Echox has %d configured skipped tests", len(skippedTests))

	// Log skipped tests for visibility
	for _, test := range skippedTests {
		t.Logf("Skipped test: %s.%s - %s", test.Interface, test.Method, test.Reason)
	}
}

// TestEchoxEngineLifecycle tests the engine start/stop lifecycle
func TestEchoxEngineLifecycle(t *testing.T) {
	engine := echox.New(echox.WithServerAddr(":0"))

	// Test initial state
	if engine.IsRunning() {
		t.Error("Engine should not be running initially")
	}

	// Note: We don't actually start/stop the engine in tests to avoid port conflicts
	// The actual lifecycle testing is handled by the Engine interface tester
	t.Log("Engine lifecycle testing delegated to Engine interface tester")
}
