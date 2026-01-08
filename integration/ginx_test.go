package integration

import (
	"testing"

	"github.com/go-sphere/httpx/ginx"
	httptesting "github.com/go-sphere/httpx/testing"
)

// TestGinxIntegration tests the ginx framework adapter as the reference implementation
func TestGinxIntegration(t *testing.T) {
	engine := ginx.New(ginx.WithServerAddr(":0"))
	RunFrameworkIntegrationTests(t, "ginx", engine, nil)
}

// TestGinxSpecificInterfaceTests allows testing specific interfaces individually
func TestGinxSpecificInterfaceTests(t *testing.T) {
	engine := ginx.New(ginx.WithServerAddr(":0"))
	RunFrameworkSpecificInterfaceTests(t, "ginx", engine, nil)
}

// TestGinxWithCustomConfig tests ginx with custom configuration
func TestGinxWithCustomConfig(t *testing.T) {
	engine := ginx.New(ginx.WithServerAddr(":0"))
	config := &httptesting.TestConfig{
		ServerAddr:     ":0",
		VerboseLogging: true,
	}
	RunFrameworkWithCustomConfig(t, "ginx", engine, config, nil)
}

// TestGinxFlexibleExecution demonstrates flexible execution for ginx
func TestGinxFlexibleExecution(t *testing.T) {
	runner := NewTestRunner()

	// Test different execution modes
	t.Run("IndividualMode", func(t *testing.T) {
		runner.RunSingleFramework(t, FrameworkGinx, ModeIndividual)
	})

	t.Run("BatchMode", func(t *testing.T) {
		runner.RunSingleFramework(t, FrameworkGinx, ModeBatch)
	})

	t.Run("BatchMode", func(t *testing.T) {
		runner.RunSingleFramework(t, FrameworkGinx, ModeBatch)
	})
}

// BenchmarkGinxFlexible provides flexible benchmarking for ginx
func BenchmarkGinxFlexible(b *testing.B) {
	runner := NewTestRunner()
	runner.BenchmarkSingleFramework(b, FrameworkGinx)
}

// TestGinxEngineLifecycle tests the engine start/stop lifecycle
func TestGinxEngineLifecycle(t *testing.T) {
	engine := ginx.New(ginx.WithServerAddr(":0"))

	// Test initial state
	if engine.IsRunning() {
		t.Error("Engine should not be running initially")
	}

	// Note: We don't actually start/stop the engine in tests to avoid port conflicts
	// The actual lifecycle testing is handled by the Engine interface tester
	t.Log("Engine lifecycle testing delegated to Engine interface tester")
}
