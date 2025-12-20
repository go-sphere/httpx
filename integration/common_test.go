package integration

import (
	"testing"

	"github.com/go-sphere/httpx"
	httpxtesting "github.com/go-sphere/httpx/testing"
)

// CommonIntegrationTests contains all the common test logic that can be reused
// across different adapter integration tests.
type CommonIntegrationTests struct {
	engine httpx.Engine
	name   string
}

// NewCommonIntegrationTests creates a new instance of common integration tests
// for the given engine and adapter name.
func NewCommonIntegrationTests(engine httpx.Engine, name string) *CommonIntegrationTests {
	return &CommonIntegrationTests{
		engine: engine,
		name:   name,
	}
}

// RunBasicIntegrationTests runs the basic integration test suite that is common
// across all adapters.
func (c *CommonIntegrationTests) RunBasicIntegrationTests(t *testing.T) {
	t.Run("EngineBasics", c.testEngineBasics)
	t.Run("RouterFunctionality", c.testRouterFunctionality)
	t.Run("AbortTracking", c.testAbortTracking)
}

// RunTestingFrameworkIntegrationTests runs the testing framework integration tests
// that are common across all adapters.
func (c *CommonIntegrationTests) RunTestingFrameworkIntegrationTests(t *testing.T) {
	t.Run("AbortTrackerIntegration", c.testAbortTrackerIntegration)
	t.Run("TestingToolsCreation", c.testTestingToolsCreation)
	t.Run("TestSuiteCreation", c.testTestSuiteCreation)
}

// RunAbortTrackingTests runs the abort tracking tests that are common
// across all adapters.
func (c *CommonIntegrationTests) RunAbortTrackingTests(t *testing.T) {
	t.Run("AbortTrackerInitialization", c.testAbortTrackerInitialization)
	t.Run("AbortTrackerReset", c.testAbortTrackerReset)
}

// RunBindingIntegrationTests runs the binding integration tests that are common
// across all adapters.
func (c *CommonIntegrationTests) RunBindingIntegrationTests(t *testing.T) {
	t.Run("HTTPBindingTests", c.testHTTPBindingTests)
	t.Run("BindingInterfaceTests", c.testBindingInterfaceTests)
}

// RunBenchmarks runs the performance benchmarks that are common across all adapters.
func (c *CommonIntegrationTests) RunBenchmarks(b *testing.B) {
	suite := httpxtesting.NewTestSuite(c.name+"-benchmark", c.engine)
	suite.RunBenchmarks(b)
}

// testEngineBasics tests basic engine functionality
func (c *CommonIntegrationTests) testEngineBasics(t *testing.T) {
	// Test that engine is not running initially
	if c.engine.IsRunning() {
		t.Error("Expected engine to not be running initially")
	}
}

// testRouterFunctionality tests router functionality
func (c *CommonIntegrationTests) testRouterFunctionality(t *testing.T) {
	// Test basic route registration
	router := c.engine.Group("/api")

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
}

// testAbortTracking tests abort tracker functionality
func (c *CommonIntegrationTests) testAbortTracking(t *testing.T) {
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
}

// testAbortTrackerIntegration tests abort tracker integration
func (c *CommonIntegrationTests) testAbortTrackerIntegration(t *testing.T) {
	tracker := httpxtesting.NewAbortTracker()

	// Test that we can set up abort testing
	httpxtesting.SetupAbortEngine(c.engine, tracker)

	// Verify tracker is properly initialized
	if len(tracker.Steps) != 0 {
		t.Error("Expected empty steps after setup")
	}

	if len(tracker.AbortedStates) != 0 {
		t.Error("Expected empty aborted states after setup")
	}

	t.Log("AbortTracker integration successful")
}

// testTestingToolsCreation tests creation of all testing tools
func (c *CommonIntegrationTests) testTestingToolsCreation(t *testing.T) {
	// Test that we can create all testing tools without errors
	requestTester := httpxtesting.NewRequestTester(c.engine)
	if requestTester == nil {
		t.Error("Failed to create RequestTester")
	}

	binderTester := httpxtesting.NewBinderTester(c.engine)
	if binderTester == nil {
		t.Error("Failed to create BinderTester")
	}

	responderTester := httpxtesting.NewResponderTester(c.engine)
	if responderTester == nil {
		t.Error("Failed to create ResponderTester")
	}

	stateStoreTester := httpxtesting.NewStateStoreTester(c.engine)
	if stateStoreTester == nil {
		t.Error("Failed to create StateStoreTester")
	}

	routerTester := httpxtesting.NewRouterTester(c.engine)
	if routerTester == nil {
		t.Error("Failed to create RouterTester")
	}

	engineTester := httpxtesting.NewEngineTester(c.engine)
	if engineTester == nil {
		t.Error("Failed to create EngineTester")
	}

	t.Log("All testing tools created successfully")
}

// testTestSuiteCreation tests creation of test suites
func (c *CommonIntegrationTests) testTestSuiteCreation(t *testing.T) {
	// Test that we can create test suites
	suite := httpxtesting.NewTestSuite(c.name+"-test", c.engine)
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

	customSuite := httpxtesting.NewTestSuiteWithConfig(c.name+"-custom", c.engine, config)
	if customSuite == nil {
		t.Error("Failed to create TestSuite with custom config")
	}

	t.Log("Test suites created successfully")
}

// testAbortTrackerInitialization tests abort tracker initialization
func (c *CommonIntegrationTests) testAbortTrackerInitialization(t *testing.T) {
	tracker := httpxtesting.NewAbortTracker()
	
	// Test initial state without setting up engine to avoid route conflicts
	if len(tracker.Steps) != 0 {
		t.Errorf("Expected empty steps on initialization, got %d", len(tracker.Steps))
	}
	if len(tracker.AbortedStates) != 0 {
		t.Errorf("Expected empty aborted states on initialization, got %d", len(tracker.AbortedStates))
	}
}

// testAbortTrackerReset tests abort tracker reset functionality
func (c *CommonIntegrationTests) testAbortTrackerReset(t *testing.T) {
	// Create a fresh tracker and engine for this test to avoid conflicts
	tracker := httpxtesting.NewAbortTracker()
	
	// Note: We don't call SetupAbortEngine here to avoid route conflicts
	// Instead, we test the tracker reset functionality directly
	
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
}

// testHTTPBindingTests tests HTTP-based binding
func (c *CommonIntegrationTests) testHTTPBindingTests(t *testing.T) {
	httpTester := httpxtesting.NewHTTPBinderTester(c.engine)
	httpTester.RunAllHTTPTests(t)
}

// testBindingInterfaceTests tests traditional binding interface
func (c *CommonIntegrationTests) testBindingInterfaceTests(t *testing.T) {
	binderTester := httpxtesting.NewBinderTester(c.engine)
	binderTester.RunAllTests(t)
}

// RunRouterTests runs router-specific tests that are common across adapters
func (c *CommonIntegrationTests) RunRouterTests(t *testing.T) {
	routerTester := httpxtesting.NewRouterTester(c.engine)

	t.Run("Handle", func(t *testing.T) {
		// Test basic route registration
		router := c.engine.Group("/test")
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
}

// RunBinderTests runs binder-specific tests that are common across adapters
func (c *CommonIntegrationTests) RunBinderTests(t *testing.T) {
	binderTester := httpxtesting.NewBinderTester(c.engine)

	t.Run("BindJSON", func(t *testing.T) {
		binderTester.TestBindJSON(t)
	})

	t.Run("BindQuery", func(t *testing.T) {
		binderTester.TestBindQuery(t)
	})

	t.Run("BindForm", func(t *testing.T) {
		binderTester.TestBindForm(t)
	})

	t.Run("BindURI", func(t *testing.T) {
		binderTester.TestBindURI(t)
	})

	t.Run("BindHeader", func(t *testing.T) {
		binderTester.TestBindHeader(t)
	})
}

// CreateExampleTestSuite creates an example test suite for documentation purposes
func (c *CommonIntegrationTests) CreateExampleTestSuite() *httpxtesting.TestSuite {
	return httpxtesting.NewTestSuite(c.name+"-example", c.engine)
}

// CreateCustomTestSuite creates a test suite with custom configuration
func (c *CommonIntegrationTests) CreateCustomTestSuite(config httpxtesting.TestConfig) *httpxtesting.TestSuite {
	return httpxtesting.NewTestSuiteWithConfig(c.name+"-custom", c.engine, config)
}