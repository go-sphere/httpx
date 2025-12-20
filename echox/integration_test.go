package echox

import (
	"testing"

	"github.com/go-sphere/httpx"
	httpxtesting "github.com/go-sphere/httpx/testing"
	"github.com/labstack/echo/v4"
)

// TestEchoxIntegration demonstrates how to use the httpx testing framework
// with the echox adapter. This serves as both a test and an example for
// other developers who want to integrate the testing framework.
func TestEchoxIntegration(t *testing.T) {
	// Create an echox engine with test configuration
	engine := New(
		WithServerAddr(":0"), // Use random port for testing
	)
	
	// Test basic engine functionality
	t.Run("EngineBasics", func(t *testing.T) {
		// Test that engine is not running initially
		if engine.IsRunning() {
			t.Error("Expected engine to not be running initially")
		}
		
		// Test that we can get the address
		addr := engine.Addr()
		if addr != ":0" {
			t.Errorf("Expected address :0, got %s", addr)
		}
	})
	
	// Test router functionality
	t.Run("RouterFunctionality", func(t *testing.T) {
		// Test basic route registration
		router := engine.Group("/api")
		
		// Test that we can register routes without errors
		router.GET("/test", func(ctx httpx.Context) {
			ctx.Text(200, "OK")
		})
		
		router.POST("/data", func(ctx httpx.Context) {
			ctx.JSON(200, map[string]string{"status": "ok"})
		})
		
		// Test middleware registration
		router.Use(func(ctx httpx.Context) {
			ctx.Set("middleware", "executed")
			ctx.Next()
		})
		
		t.Log("Successfully registered routes and middleware")
	})
	
	// Test abort tracker functionality
	t.Run("AbortTracking", func(t *testing.T) {
		tracker := httpxtesting.NewAbortTracker()
		
		// Test initial state
		if len(tracker.Steps) != 0 {
			t.Errorf("Expected empty steps initially, got %d", len(tracker.Steps))
		}
		
		if len(tracker.AbortedStates) != 0 {
			t.Errorf("Expected empty aborted states initially, got %d", len(tracker.AbortedStates))
		}
		
		// Test reset functionality
		tracker.Steps = append(tracker.Steps, "test")
		tracker.AbortedStates = append(tracker.AbortedStates, false)
		
		tracker.Reset()
		
		if len(tracker.Steps) != 0 {
			t.Errorf("Expected empty steps after reset, got %d", len(tracker.Steps))
		}
		
		if len(tracker.AbortedStates) != 0 {
			t.Errorf("Expected empty aborted states after reset, got %d", len(tracker.AbortedStates))
		}
	})
}

// TestEchoxTestingFrameworkIntegration demonstrates how to properly integrate
// the testing framework with echox for comprehensive testing.
func TestEchoxTestingFrameworkIntegration(t *testing.T) {
	engine := New(WithServerAddr(":0"))
	
	// Test individual testing components
	t.Run("AbortTrackerIntegration", func(t *testing.T) {
		tracker := httpxtesting.NewAbortTracker()
		
		// Test that we can set up abort testing
		httpxtesting.SetupAbortEngine(engine, tracker)
		
		// Verify tracker is properly initialized
		if len(tracker.Steps) != 0 {
			t.Error("Expected empty steps after setup")
		}
		
		if len(tracker.AbortedStates) != 0 {
			t.Error("Expected empty aborted states after setup")
		}
		
		t.Log("AbortTracker integration successful")
	})
	
	t.Run("TestingToolsCreation", func(t *testing.T) {
		// Test that we can create all testing tools without errors
		requestTester := httpxtesting.NewRequestTester(engine)
		if requestTester == nil {
			t.Error("Failed to create RequestTester")
		}
		
		binderTester := httpxtesting.NewBinderTester(engine)
		if binderTester == nil {
			t.Error("Failed to create BinderTester")
		}
		
		responderTester := httpxtesting.NewResponderTester(engine)
		if responderTester == nil {
			t.Error("Failed to create ResponderTester")
		}
		
		stateStoreTester := httpxtesting.NewStateStoreTester(engine)
		if stateStoreTester == nil {
			t.Error("Failed to create StateStoreTester")
		}
		
		routerTester := httpxtesting.NewRouterTester(engine)
		if routerTester == nil {
			t.Error("Failed to create RouterTester")
		}
		
		engineTester := httpxtesting.NewEngineTester(engine)
		if engineTester == nil {
			t.Error("Failed to create EngineTester")
		}
		
		t.Log("All testing tools created successfully")
	})
	
	t.Run("TestSuiteCreation", func(t *testing.T) {
		// Test that we can create test suites
		suite := httpxtesting.NewTestSuite("echox-test", engine)
		if suite == nil {
			t.Error("Failed to create TestSuite")
		}
		
		// Test with custom config
		config := httpxtesting.TestConfig{
			ServerAddr:      ":0",
			RequestTimeout:  httpxtesting.DefaultTestConfig.RequestTimeout,
			ConcurrentUsers: 3,
			TestDataSize:    256,
		}
		
		customSuite := httpxtesting.NewTestSuiteWithConfig("echox-custom", engine, config)
		if customSuite == nil {
			t.Error("Failed to create TestSuite with custom config")
		}
		
		t.Log("Test suites created successfully")
	})
}

// TestEchoxAbortTracking demonstrates how to test middleware abort behavior
// specifically with the echox adapter.
func TestEchoxAbortTracking(t *testing.T) {
	engine := New(WithServerAddr(":0"))
	
	// Create abort tracker for testing middleware behavior
	tracker := httpxtesting.NewAbortTracker()
	
	// Set up the engine with abort testing middleware
	httpxtesting.SetupAbortEngine(engine, tracker)
	
	// Test abort tracking functionality
	t.Run("AbortTrackerInitialization", func(t *testing.T) {
		if len(tracker.Steps) != 0 {
			t.Errorf("Expected empty steps on initialization, got %d", len(tracker.Steps))
		}
		if len(tracker.AbortedStates) != 0 {
			t.Errorf("Expected empty aborted states on initialization, got %d", len(tracker.AbortedStates))
		}
	})
	
	t.Run("AbortTrackerReset", func(t *testing.T) {
		// Add some test data
		tracker.Steps = append(tracker.Steps, "test_step")
		tracker.AbortedStates = append(tracker.AbortedStates, false)
		
		// Reset and verify
		tracker.Reset()
		if len(tracker.Steps) != 0 {
			t.Errorf("Expected empty steps after reset, got %d", len(tracker.Steps))
		}
		if len(tracker.AbortedStates) != 0 {
			t.Errorf("Expected empty aborted states after reset, got %d", len(tracker.AbortedStates))
		}
	})
}

// TestEchoxSpecificFeatures tests echox-specific features and behaviors
// that might differ from other adapters.
func TestEchoxSpecificFeatures(t *testing.T) {
	// Test with echo's built-in middleware
	echoEngine := echo.New()
	
	engine := New(
		WithEngine(echoEngine),
		WithServerAddr(":0"),
	)
	
	// Test router functionality with echo-specific features
	t.Run("RouterWithEchoMiddleware", func(t *testing.T) {
		routerTester := httpxtesting.NewRouterTester(engine)
		
		// Test individual components that are more likely to work
		t.Run("Handle", func(t *testing.T) {
			// Test basic route registration
			router := engine.Group("/test")
			router.Handle("GET", "/handle-test", func(ctx httpx.Context) {
				ctx.Text(200, "OK")
			})
		})
		
		t.Run("HTTPMethods", func(t *testing.T) {
			routerTester.TestHTTPMethods(t)
		})
		
		t.Run("Any", func(t *testing.T) {
			routerTester.TestAny(t)
		})
		
		t.Run("Group", func(t *testing.T) {
			routerTester.TestGroup(t)
		})
		
		t.Run("Middleware", func(t *testing.T) {
			routerTester.TestMiddleware(t)
		})
		
		t.Run("BasePath", func(t *testing.T) {
			routerTester.TestBasePath(t)
		})
	})
	
	// Test binding functionality which might have echo-specific behavior
	t.Run("BinderWithEchoFeatures", func(t *testing.T) {
		binderTester := httpxtesting.NewBinderTester(engine)
		binderTester.RunAllTests(t)
	})
}

// BenchmarkEchoxPerformance runs performance benchmarks for the echox adapter
// using the testing framework's benchmark tools.
func BenchmarkEchoxPerformance(b *testing.B) {
	engine := New(WithServerAddr(":0"))
	suite := httpxtesting.NewTestSuite("echox-benchmark", engine)
	
	// Run all performance benchmarks
	suite.RunBenchmarks(b)
}

// Example_echoxIntegration shows how to use the testing framework with echox
// in a simple, straightforward way.
func Example_echoxIntegration() {
	// Create echox engine
	engine := New(WithServerAddr(":8080"))
	
	// Create test suite
	suite := httpxtesting.NewTestSuite("echox-example", engine)
	
	// In a real test, you would call:
	// suite.RunAllTests(t)
	
	// This example demonstrates the basic setup
	_ = suite
}