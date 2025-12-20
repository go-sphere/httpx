package integration

import (
	"testing"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/go-sphere/httpx"
	"github.com/go-sphere/httpx/hertzx"
	httpxtesting "github.com/go-sphere/httpx/testing"
)

// TestHertzxIntegration demonstrates how to use the httpx testing framework
// with the hertzx adapter. This serves as both a test and an example for
// other developers who want to integrate the testing framework.
func TestHertzxIntegration(t *testing.T) {
	// Create a hertzx engine with test configuration
	engine := hertzx.New()

	// Test basic engine functionality
	t.Run("EngineBasics", func(t *testing.T) {
		// Test that engine is not running initially
		if engine.IsRunning() {
			t.Error("Expected engine to not be running initially")
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

// TestHertzxTestingFrameworkIntegration demonstrates how to properly integrate
// the testing framework with hertzx for comprehensive testing.
func TestHertzxTestingFrameworkIntegration(t *testing.T) {
	engine := hertzx.New()

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
		suite := httpxtesting.NewTestSuite("hertzx-test", engine)
		if suite == nil {
			t.Error("Failed to create TestSuite")
		}

		// Test with custom config
		config := httpxtesting.TestConfig{
			ServerAddr:      ":8888", // Use hertz default port
			RequestTimeout:  httpxtesting.DefaultTestConfig.RequestTimeout,
			ConcurrentUsers: 3,
			TestDataSize:    256,
		}

		customSuite := httpxtesting.NewTestSuiteWithConfig("hertzx-custom", engine, config)
		if customSuite == nil {
			t.Error("Failed to create TestSuite with custom config")
		}

		t.Log("Test suites created successfully")
	})
}

// TestHertzxAbortTracking demonstrates how to test middleware abort behavior
// specifically with the hertzx adapter.
func TestHertzxAbortTracking(t *testing.T) {
	engine := hertzx.New()

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

// TestHertzxSpecificFeatures tests hertzx-specific features and behaviors
// that might differ from other adapters. Note that hertzx is a minimal-level
// adaptation, so some features may have limitations.
func TestHertzxSpecificFeatures(t *testing.T) {
	// Test with hertz's built-in configuration
	hertzEngine := server.Default()

	engine := hertzx.New(
		hertzx.WithEngine(hertzEngine),
	)

	// Test router functionality with hertz-specific features
	t.Run("RouterWithHertzFeatures", func(t *testing.T) {
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

		// Note: BasePath test might have limitations due to minimal adaptation
		t.Run("BasePath", func(t *testing.T) {
			routerTester.TestBasePath(t)
		})
	})

	// Test binding functionality - hertzx may have limitations
	t.Run("BinderWithHertzFeatures", func(t *testing.T) {
		binderTester := httpxtesting.NewBinderTester(engine)
		// Run individual tests to better handle potential limitations
		t.Run("BindJSON", func(t *testing.T) {
			binderTester.TestBindJSON(t)
		})

		t.Run("BindQuery", func(t *testing.T) {
			binderTester.TestBindQuery(t)
		})

		t.Run("BindForm", func(t *testing.T) {
			binderTester.TestBindForm(t)
		})

		// URI and Header binding might have limitations in hertzx
		t.Run("BindURI", func(t *testing.T) {
			binderTester.TestBindURI(t)
		})

		t.Run("BindHeader", func(t *testing.T) {
			binderTester.TestBindHeader(t)
		})
	})
}

// BenchmarkHertzxPerformance runs performance benchmarks for the hertzx adapter
// using the testing framework's benchmark tools.
func BenchmarkHertzxPerformance(b *testing.B) {
	engine := hertzx.New()
	suite := httpxtesting.NewTestSuite("hertzx-benchmark", engine)

	// Run all performance benchmarks
	suite.RunBenchmarks(b)
}

// TestHertzxBindingIntegration tests hertzx binding functionality with real HTTP requests
func TestHertzxBindingIntegration(t *testing.T) {
	engine := hertzx.New()

	// Test HTTP-based binding
	t.Run("HTTPBindingTests", func(t *testing.T) {
		httpTester := httpxtesting.NewHTTPBinderTester(engine)
		httpTester.RunAllHTTPTests(t)
	})

	// Test traditional binding interface
	t.Run("BindingInterfaceTests", func(t *testing.T) {
		binderTester := httpxtesting.NewBinderTester(engine)
		binderTester.RunAllTests(t)
	})
}

// Example_hertzxIntegration shows how to use the testing framework with hertzx
// in a simple, straightforward way.
func Example_hertzxIntegration() {
	// Create hertzx engine
	engine := hertzx.New()

	// Create test suite
	suite := httpxtesting.NewTestSuite("hertzx-example", engine)

	// In a real test, you would call:
	// suite.RunAllTests(t)

	// This example demonstrates the basic setup
	_ = suite
}
