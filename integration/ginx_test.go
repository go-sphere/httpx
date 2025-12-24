package integration

import (
	"testing"

	"github.com/go-sphere/httpx/ginx"
	httptesting "github.com/go-sphere/httpx/testing"
)

// TestGinxIntegration tests the ginx framework adapter as the reference implementation
func TestGinxIntegration(t *testing.T) {
	// Create ginx engine with test configuration
	engine := ginx.New(ginx.WithServerAddr(":0"))
	
	// Create common integration tests instance
	cit := NewCommonIntegrationTests("ginx", engine)
	
	// Validate framework integration first
	t.Run("ValidateIntegration", func(t *testing.T) {
		cit.ValidateFrameworkIntegration(t)
	})
	
	// Run all interface tests - ginx should pass all tests as reference implementation
	t.Run("AllInterfaceTests", func(t *testing.T) {
		cit.RunAllInterfaceTests(t)
	})
	
	// Run individual interface tests for better isolation and debugging
	t.Run("IndividualInterfaceTests", func(t *testing.T) {
		cit.RunIndividualInterfaceTests(t)
	})
}

// TestGinxSpecificInterfaceTests allows testing specific interfaces individually
func TestGinxSpecificInterfaceTests(t *testing.T) {
	engine := ginx.New(ginx.WithServerAddr(":0"))
	cit := NewCommonIntegrationTests("ginx", engine)
	
	// Test each interface individually - useful for debugging specific issues
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
			cit.RunSpecificInterfaceTest(t, interfaceName)
		})
	}
}

// TestGinxWithCustomConfig tests ginx with custom configuration
func TestGinxWithCustomConfig(t *testing.T) {
	engine := ginx.New(ginx.WithServerAddr(":0"))
	
	// Create custom test configuration
	config := &httptesting.TestConfig{
		ServerAddr:     ":0",
		VerboseLogging: true,
	}
	
	cit := NewCommonIntegrationTestsWithConfig("ginx", engine, config)
	
	t.Run("CustomConfigTests", func(t *testing.T) {
		cit.RunAllInterfaceTests(t)
	})
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
	
	t.Run("ValidationMode", func(t *testing.T) {
		runner.RunSingleFramework(t, FrameworkGinx, ModeValidation)
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