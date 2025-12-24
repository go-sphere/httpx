package integration

import (
	"testing"

	"github.com/go-sphere/httpx/echox"
	httptesting "github.com/go-sphere/httpx/testing"
)

// setupEchoxSkipManager configures known failing tests for echox
func setupEchoxSkipManager() *TestSkipManager {
	skipManager := NewTestSkipManager()
	
	// Add known failing tests for echox - these should be updated as issues are fixed
	// Example skipped tests (these may need to be adjusted based on actual test results):
	
	// Uncomment and adjust these as needed based on actual test failures:
	// skipManager.AddSkippedTest("echox", "Binder", "BindURI", "URI parameter binding differences")
	// skipManager.AddSkippedTest("echox", "RequestInfo", "Params", "Parameter handling differences")
	// skipManager.AddSkippedTest("echox", "Responder", "SetCookie", "Cookie handling differences")
	
	return skipManager
}

// TestEchoxIntegration tests the echox framework adapter with skip support
func TestEchoxIntegration(t *testing.T) {
	// Create echox engine with test configuration
	engine := echox.New(echox.WithServerAddr(":0"))
	
	// Create common integration tests instance
	cit := NewCommonIntegrationTests("echox", engine)
	
	// Set up skip manager for known failing tests
	skipManager := setupEchoxSkipManager()
	
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

// TestEchoxSpecificInterfaceTests allows testing specific interfaces individually with skip support
func TestEchoxSpecificInterfaceTests(t *testing.T) {
	engine := echox.New(echox.WithServerAddr(":0"))
	cit := NewCommonIntegrationTests("echox", engine)
	skipManager := setupEchoxSkipManager()
	
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

// TestEchoxWithCustomConfig tests echox with custom configuration
func TestEchoxWithCustomConfig(t *testing.T) {
	engine := echox.New(echox.WithServerAddr(":0"))
	
	// Create custom test configuration
	config := &httptesting.TestConfig{
		ServerAddr:     ":0",
		VerboseLogging: true,
	}
	
	cit := NewCommonIntegrationTestsWithConfig("echox", engine, config)
	skipManager := setupEchoxSkipManager()
	
	t.Run("CustomConfigTests", func(t *testing.T) {
		// Run tests with skip support
		cit.RunWithSkipSupport(t, skipManager, "all", func(t *testing.T) {
			cit.RunAllInterfaceTests(t)
		})
	})
}

// BenchmarkEchoxIntegration provides performance benchmarks for echox
func BenchmarkEchoxIntegration(b *testing.B) {
	engine := echox.New(echox.WithServerAddr(":0"))
	cit := NewCommonIntegrationTests("echox", engine)
	
	// Benchmark interface tests
	cit.BenchmarkInterfaceTests(b)
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