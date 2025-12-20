package ginx

import (
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/go-sphere/httpx"
	httpxtesting "github.com/go-sphere/httpx/testing"
)

// TestGinxIntegration demonstrates how to use the httpx testing framework
// with the ginx adapter. This serves as both a test and an example for
// other developers who want to integrate the testing framework.
func TestGinxIntegration(t *testing.T) {
	// Set gin to test mode to reduce noise in test output
	gin.SetMode(gin.TestMode)
	
	// Create a ginx engine with test configuration
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

// TestGinxTestingFrameworkIntegration demonstrates how to properly integrate
// the testing framework with ginx for comprehensive testing.
func TestGinxTestingFrameworkIntegration(t *testing.T) {
	gin.SetMode(gin.TestMode)
	
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
		suite := httpxtesting.NewTestSuite("ginx-test", engine)
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
		
		customSuite := httpxtesting.NewTestSuiteWithConfig("ginx-custom", engine, config)
		if customSuite == nil {
			t.Error("Failed to create TestSuite with custom config")
		}
		
		t.Log("Test suites created successfully")
	})
}

// TestGinxAbortTracking demonstrates how to test middleware abort behavior
// specifically with the ginx adapter.
func TestGinxAbortTracking(t *testing.T) {
	gin.SetMode(gin.TestMode)
	
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

// TestGinxSpecificFeatures tests ginx-specific features and behaviors
// that might differ from other adapters.
func TestGinxSpecificFeatures(t *testing.T) {
	gin.SetMode(gin.TestMode)
	
	// Test with gin's built-in middleware
	ginEngine := gin.New()
	ginEngine.Use(gin.Logger(), gin.Recovery())
	
	engine := New(
		WithEngine(ginEngine),
		WithServerAddr(":0"),
	)
	
	// Test router functionality with gin-specific features
	t.Run("RouterWithGinMiddleware", func(t *testing.T) {
		routerTester := httpxtesting.NewRouterTester(engine)
		routerTester.RunAllTests(t)
	})
	
	// Test binding functionality which might have gin-specific behavior
	t.Run("BinderWithGinFeatures", func(t *testing.T) {
		binderTester := httpxtesting.NewBinderTester(engine)
		binderTester.RunAllTests(t)
	})
}

// BenchmarkGinxPerformance runs performance benchmarks for the ginx adapter
// using the testing framework's benchmark tools.
func BenchmarkGinxPerformance(b *testing.B) {
	gin.SetMode(gin.TestMode)
	
	engine := New(WithServerAddr(":0"))
	suite := httpxtesting.NewTestSuite("ginx-benchmark", engine)
	
	// Run all performance benchmarks
	suite.RunBenchmarks(b)
}

// Example_ginxIntegration shows how to use the testing framework with ginx
// in a simple, straightforward way.
func Example_ginxIntegration() {
	// Set gin to test mode
	gin.SetMode(gin.TestMode)
	
	// Create ginx engine
	engine := New(WithServerAddr(":8080"))
	
	// Create test suite
	suite := httpxtesting.NewTestSuite("ginx-example", engine)
	
	// In a real test, you would call:
	// suite.RunAllTests(t)
	
	// This example demonstrates the basic setup
	_ = suite
}