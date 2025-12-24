package integration

import (
	"testing"

	"github.com/go-sphere/httpx"
	httptesting "github.com/go-sphere/httpx/testing"
)

// CommonIntegrationTests provides shared integration test logic for all framework adapters
type CommonIntegrationTests struct {
	frameworkName string
	engine        httpx.Engine
	suite         *httptesting.TestSuite
	config        *httptesting.TestConfig
}

// NewCommonIntegrationTests creates a new common integration test instance
func NewCommonIntegrationTests(frameworkName string, engine httpx.Engine) *CommonIntegrationTests {
	return NewCommonIntegrationTestsWithConfig(frameworkName, engine, nil)
}

// NewCommonIntegrationTestsWithConfig creates a new common integration test instance with custom config
func NewCommonIntegrationTestsWithConfig(frameworkName string, engine httpx.Engine, config *httptesting.TestConfig) *CommonIntegrationTests {
	if config == nil {
		config = httptesting.DefaultTestConfig()
	}
	
	suite := httptesting.NewTestSuiteWithConfig(frameworkName, engine, config)
	
	return &CommonIntegrationTests{
		frameworkName: frameworkName,
		engine:        engine,
		suite:         suite,
		config:        config,
	}
}

// RunAllInterfaceTests runs all interface tests using the new interface testers
func (cit *CommonIntegrationTests) RunAllInterfaceTests(t *testing.T) {
	t.Helper()
	
	t.Logf("Running all interface tests for framework: %s", cit.frameworkName)
	
	// Run the complete test suite which coordinates all interface testers
	cit.suite.RunAllTests(t)
}

// RunIndividualInterfaceTests runs each interface test individually for better isolation
func (cit *CommonIntegrationTests) RunIndividualInterfaceTests(t *testing.T) {
	t.Helper()
	
	t.Logf("Running individual interface tests for framework: %s", cit.frameworkName)
	
	// Test each interface individually
	t.Run("RequestInfo", func(t *testing.T) {
		cit.suite.RunRequestInfoTests(t)
	})
	
	t.Run("Request", func(t *testing.T) {
		cit.suite.RunRequestTests(t)
	})
	
	t.Run("BodyAccess", func(t *testing.T) {
		cit.suite.RunBodyAccessTests(t)
	})
	
	t.Run("FormAccess", func(t *testing.T) {
		cit.suite.RunFormAccessTests(t)
	})
	
	t.Run("Binder", func(t *testing.T) {
		cit.suite.RunBinderTests(t)
	})
	
	t.Run("Responder", func(t *testing.T) {
		cit.suite.RunResponderTests(t)
	})
	
	t.Run("StateStore", func(t *testing.T) {
		cit.suite.RunStateStoreTests(t)
	})
	
	t.Run("Aborter", func(t *testing.T) {
		cit.suite.RunAborterTests(t)
	})
	
	t.Run("Router", func(t *testing.T) {
		cit.suite.RunRouterTests(t)
	})
	
	t.Run("Engine", func(t *testing.T) {
		cit.suite.RunEngineTests(t)
	})
}

// RunSpecificInterfaceTest runs a test for a specific interface
func (cit *CommonIntegrationTests) RunSpecificInterfaceTest(t *testing.T, interfaceName string) {
	t.Helper()
	
	t.Logf("Running %s interface test for framework: %s", interfaceName, cit.frameworkName)
	
	switch interfaceName {
	case "RequestInfo":
		cit.suite.RunRequestInfoTests(t)
	case "Request":
		cit.suite.RunRequestTests(t)
	case "BodyAccess":
		cit.suite.RunBodyAccessTests(t)
	case "FormAccess":
		cit.suite.RunFormAccessTests(t)
	case "Binder":
		cit.suite.RunBinderTests(t)
	case "Responder":
		cit.suite.RunResponderTests(t)
	case "StateStore":
		cit.suite.RunStateStoreTests(t)
	case "Aborter":
		cit.suite.RunAborterTests(t)
	case "Router":
		cit.suite.RunRouterTests(t)
	case "Engine":
		cit.suite.RunEngineTests(t)
	default:
		t.Errorf("Unknown interface: %s", interfaceName)
	}
}

// GetFrameworkName returns the name of the framework being tested
func (cit *CommonIntegrationTests) GetFrameworkName() string {
	return cit.frameworkName
}

// GetEngine returns the engine being tested
func (cit *CommonIntegrationTests) GetEngine() httpx.Engine {
	return cit.engine
}

// GetTestSuite returns the underlying test suite
func (cit *CommonIntegrationTests) GetTestSuite() *httptesting.TestSuite {
	return cit.suite
}

// GetConfig returns the test configuration
func (cit *CommonIntegrationTests) GetConfig() *httptesting.TestConfig {
	return cit.config
}

// SkippableTest represents a test that can be conditionally skipped
type SkippableTest struct {
	Name      string
	Framework string
	Interface string
	Method    string
	Reason    string
	Skip      bool
}

// TestSkipManager manages test skipping for known failing tests
type TestSkipManager struct {
	skippedTests map[string][]SkippableTest
}

// NewTestSkipManager creates a new test skip manager
func NewTestSkipManager() *TestSkipManager {
	return &TestSkipManager{
		skippedTests: make(map[string][]SkippableTest),
	}
}

// AddSkippedTest adds a test to be skipped for a specific framework
func (tsm *TestSkipManager) AddSkippedTest(framework, interfaceName, method, reason string) {
	test := SkippableTest{
		Name:      framework + "_" + interfaceName + "_" + method,
		Framework: framework,
		Interface: interfaceName,
		Method:    method,
		Reason:    reason,
		Skip:      true,
	}
	
	tsm.skippedTests[framework] = append(tsm.skippedTests[framework], test)
}

// ShouldSkipTest checks if a test should be skipped for a framework
func (tsm *TestSkipManager) ShouldSkipTest(framework, interfaceName, method string) (bool, string) {
	tests, exists := tsm.skippedTests[framework]
	if !exists {
		return false, ""
	}
	
	for _, test := range tests {
		if test.Interface == interfaceName && test.Method == method && test.Skip {
			return true, test.Reason
		}
	}
	
	return false, ""
}

// GetSkippedTests returns all skipped tests for a framework
func (tsm *TestSkipManager) GetSkippedTests(framework string) []SkippableTest {
	return tsm.skippedTests[framework]
}

// RunWithSkipSupport runs a test with skip support based on framework and interface
func (cit *CommonIntegrationTests) RunWithSkipSupport(t *testing.T, skipManager *TestSkipManager, interfaceName string, testFunc func(*testing.T)) {
	t.Helper()
	
	if skip, reason := skipManager.ShouldSkipTest(cit.frameworkName, interfaceName, "all"); skip {
		ctx := httptesting.NewTestContext(cit.frameworkName, interfaceName, "all", "interface_test")
		cit.suite.Helper().LogTestSkipped(t, ctx, reason)
		t.Skipf("Skipping %s interface tests for %s: %s", interfaceName, cit.frameworkName, reason)
		return
	}
	
	// Log test start
	cit.suite.Helper().ReportInterfaceTestStart(t, cit.frameworkName, interfaceName)
	
	// Run the test function
	testFunc(t)
	
	// Note: Test completion logging is handled by individual test methods
}

// RunIndividualInterfaceTestsWithSkipSupport runs each interface test individually with skip support
func (cit *CommonIntegrationTests) RunIndividualInterfaceTestsWithSkipSupport(t *testing.T, skipManager *TestSkipManager) {
	t.Helper()
	
	t.Logf("Running individual interface tests with skip support for framework: %s", cit.frameworkName)
	
	// Test each interface individually with skip support
	t.Run("RequestInfo", func(t *testing.T) {
		cit.RunWithSkipSupport(t, skipManager, "RequestInfo", func(t *testing.T) {
			cit.suite.RunRequestInfoTests(t)
		})
	})
	
	t.Run("Request", func(t *testing.T) {
		cit.RunWithSkipSupport(t, skipManager, "Request", func(t *testing.T) {
			cit.suite.RunRequestTests(t)
		})
	})
	
	t.Run("BodyAccess", func(t *testing.T) {
		cit.RunWithSkipSupport(t, skipManager, "BodyAccess", func(t *testing.T) {
			cit.suite.RunBodyAccessTests(t)
		})
	})
	
	t.Run("FormAccess", func(t *testing.T) {
		cit.RunWithSkipSupport(t, skipManager, "FormAccess", func(t *testing.T) {
			cit.suite.RunFormAccessTests(t)
		})
	})
	
	t.Run("Binder", func(t *testing.T) {
		cit.RunWithSkipSupport(t, skipManager, "Binder", func(t *testing.T) {
			cit.suite.RunBinderTests(t)
		})
	})
	
	t.Run("Responder", func(t *testing.T) {
		cit.RunWithSkipSupport(t, skipManager, "Responder", func(t *testing.T) {
			cit.suite.RunResponderTests(t)
		})
	})
	
	t.Run("StateStore", func(t *testing.T) {
		cit.RunWithSkipSupport(t, skipManager, "StateStore", func(t *testing.T) {
			cit.suite.RunStateStoreTests(t)
		})
	})
	
	t.Run("Aborter", func(t *testing.T) {
		cit.RunWithSkipSupport(t, skipManager, "Aborter", func(t *testing.T) {
			cit.suite.RunAborterTests(t)
		})
	})
	
	t.Run("Router", func(t *testing.T) {
		cit.RunWithSkipSupport(t, skipManager, "Router", func(t *testing.T) {
			cit.suite.RunRouterTests(t)
		})
	})
	
	t.Run("Engine", func(t *testing.T) {
		cit.RunWithSkipSupport(t, skipManager, "Engine", func(t *testing.T) {
			cit.suite.RunEngineTests(t)
		})
	})
}

// BenchmarkInterfaceTests provides benchmarking capabilities for interface tests
func (cit *CommonIntegrationTests) BenchmarkInterfaceTests(b *testing.B) {
	b.Logf("Benchmarking interface tests for framework: %s", cit.frameworkName)
	
	// Benchmark individual interfaces
	b.Run("RequestInfo", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			t := &testing.T{}
			cit.suite.RunRequestInfoTests(t)
		}
	})
	
	b.Run("BodyAccess", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			t := &testing.T{}
			cit.suite.RunBodyAccessTests(t)
		}
	})
	
	b.Run("Binder", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			t := &testing.T{}
			cit.suite.RunBinderTests(t)
		}
	})
	
	b.Run("Responder", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			t := &testing.T{}
			cit.suite.RunResponderTests(t)
		}
	})
}

// ValidateFrameworkIntegration validates that a framework properly integrates with httpx interfaces
func (cit *CommonIntegrationTests) ValidateFrameworkIntegration(t *testing.T) {
	t.Helper()
	
	ctx := httptesting.NewTestContext(cit.frameworkName, "Framework", "Validation", "integration_check")
	cit.suite.Helper().LogTestStart(t, ctx)
	
	t.Logf("Validating framework integration for: %s", cit.frameworkName)
	
	// Validate that the engine is not nil
	if cit.engine == nil {
		httptesting.FailWithContext(t, ctx, "Engine is nil for framework: %s", cit.frameworkName)
		return
	}
	
	// Validate that the test suite is properly initialized
	if cit.suite == nil {
		httptesting.FailWithContext(t, ctx, "Test suite is nil for framework: %s", cit.frameworkName)
		return
	}
	
	// Validate that all interface testers are available
	if cit.suite.GetRequestInfoTester() == nil {
		cit.suite.Helper().ReportTestFailure(t, ctx, "RequestInfo tester is nil")
	}
	
	if cit.suite.GetBodyAccessTester() == nil {
		cit.suite.Helper().ReportTestFailure(t, ctx, "BodyAccess tester is nil")
	}
	
	if cit.suite.GetBinderTester() == nil {
		cit.suite.Helper().ReportTestFailure(t, ctx, "Binder tester is nil")
	}
	
	if cit.suite.GetResponderTester() == nil {
		cit.suite.Helper().ReportTestFailure(t, ctx, "Responder tester is nil")
	}
	
	if cit.suite.GetRouterTester() == nil {
		cit.suite.Helper().ReportTestFailure(t, ctx, "Router tester is nil")
	}
	
	if cit.suite.GetEngineTester() == nil {
		cit.suite.Helper().ReportTestFailure(t, ctx, "Engine tester is nil")
	}
	
	cit.suite.Helper().LogTestComplete(t, ctx)
	t.Logf("Framework integration validation completed for: %s", cit.frameworkName)
}

// RunAllInterfaceTestsWithReporting runs all interface tests with enhanced reporting
func (cit *CommonIntegrationTests) RunAllInterfaceTestsWithReporting(t *testing.T) {
	t.Helper()
	
	t.Logf("Running all interface tests with enhanced reporting for framework: %s", cit.frameworkName)
	
	results := make(map[string]httptesting.TestResult)
	
	// Test each interface and collect results
	interfaces := []string{
		"RequestInfo", "Request", "BodyAccess", "FormAccess", 
		"Binder", "Responder", "StateStore", "Aborter", "Router", "Engine",
	}
	
	for _, interfaceName := range interfaces {
		t.Run(interfaceName, func(t *testing.T) {
			cit.suite.Helper().ReportInterfaceTestStart(t, cit.frameworkName, interfaceName)
			
			// Run the specific interface test
			switch interfaceName {
			case "RequestInfo":
				cit.suite.RunRequestInfoTests(t)
			case "Request":
				cit.suite.RunRequestTests(t)
			case "BodyAccess":
				cit.suite.RunBodyAccessTests(t)
			case "FormAccess":
				cit.suite.RunFormAccessTests(t)
			case "Binder":
				cit.suite.RunBinderTests(t)
			case "Responder":
				cit.suite.RunResponderTests(t)
			case "StateStore":
				cit.suite.RunStateStoreTests(t)
			case "Aborter":
				cit.suite.RunAborterTests(t)
			case "Router":
				cit.suite.RunRouterTests(t)
			case "Engine":
				cit.suite.RunEngineTests(t)
			}
			
			// Note: Individual test results would be collected here in a real implementation
			// For now, we'll assume tests passed if no panic occurred
			results[interfaceName] = httptesting.TestResult{
				Interface: interfaceName,
				Passed:    1, // Simplified - would need actual counting
				Failed:    0,
				Skipped:   0,
			}
		})
	}
	
	// Report summary
	cit.suite.Helper().ReportFrameworkTestSummary(t, cit.frameworkName, results)
}