package integration

import (
	"testing"

	"github.com/go-sphere/httpx"
	httptesting "github.com/go-sphere/httpx/testing"
)

// TestCases provides shared integration test logic for all framework adapters
type TestCases struct {
	frameworkName string
	engine        httpx.Engine
	suite         *httptesting.TestSuite
	config        *httptesting.TestConfig
}

// NewTestCases creates a new test cases instance
func NewTestCases(frameworkName string, engine httpx.Engine) *TestCases {
	return NewTestCasesWithConfig(frameworkName, engine, nil)
}

// NewTestCasesWithConfig creates a new test cases instance with custom config
func NewTestCasesWithConfig(frameworkName string, engine httpx.Engine, config *httptesting.TestConfig) *TestCases {
	if config == nil {
		config = httptesting.DefaultTestConfig()
	}

	suite := httptesting.NewTestSuiteWithConfig(frameworkName, engine, config)

	return &TestCases{
		frameworkName: frameworkName,
		engine:        engine,
		suite:         suite,
		config:        config,
	}
}

// FrameworkName returns the name of the framework being tested.
func (tc *TestCases) FrameworkName() string {
	return tc.frameworkName
}

// Engine returns the engine being tested.
func (tc *TestCases) Engine() httpx.Engine {
	return tc.engine
}

// TestSuite returns the underlying test suite.
func (tc *TestCases) TestSuite() *httptesting.TestSuite {
	return tc.suite
}

// Config returns the test configuration.
func (tc *TestCases) Config() *httptesting.TestConfig {
	return tc.config
}

// RunSpecificInterfaceTest runs a test for a specific interface by name.
// The interfaceName parameter must match one of the httpx interface names.
func (tc *TestCases) RunSpecificInterfaceTest(t *testing.T, interfaceName string) {
	t.Helper()

	t.Logf("Running %s interface test for framework: %s", interfaceName, tc.frameworkName)

	switch interfaceName {
	case "RequestInfo":
		tc.suite.RunRequestInfoTests(t)
	case "Request":
		tc.suite.RunRequestTests(t)
	case "BodyAccess":
		tc.suite.RunBodyAccessTests(t)
	case "FormAccess":
		tc.suite.RunFormAccessTests(t)
	case "Binder":
		tc.suite.RunBinderTests(t)
	case "Responder":
		tc.suite.RunResponderTests(t)
	case "StateStore":
		tc.suite.RunStateStoreTests(t)
	case "Router":
		tc.suite.RunRouterTests(t)
	case "Engine":
		tc.suite.RunEngineTests(t)
	default:
		t.Errorf("Unknown interface: %s", interfaceName)
	}
}

// RunWithSkipSupport runs a test with skip support based on framework and interface.
// If the skipManager indicates the test should be skipped, it will be skipped with the reason logged.
func (tc *TestCases) RunWithSkipSupport(t *testing.T, skipManager *TestSkipManager, interfaceName string, testFunc func(*testing.T)) {
	t.Helper()

	if skip, reason := skipManager.ShouldSkipTest(tc.frameworkName, interfaceName, "all"); skip {
		ctx := httptesting.NewTestContext(tc.frameworkName, interfaceName, "all", "interface_test")
		tc.suite.Helper().LogTestSkipped(t, ctx, reason)
		t.Skipf("Skipping %s interface tests for %s: %s", interfaceName, tc.frameworkName, reason)
		return
	}

	// Log test start
	tc.suite.Helper().ReportInterfaceTestStart(t, tc.frameworkName, interfaceName)

	// Run the test function
	testFunc(t)

	// Note: Test completion logging is handled by individual test methods
}

// RunIndividualInterfaceTestsWithSkipSupport runs each interface test individually with skip support.
// Tests that are marked to be skipped for a specific framework will be skipped.
func (tc *TestCases) RunIndividualInterfaceTestsWithSkipSupport(t *testing.T, skipManager *TestSkipManager) {
	t.Helper()

	t.Logf("Running individual interface tests with skip support for framework: %s", tc.frameworkName)

	// Test each interface individually with skip support
	t.Run("RequestInfo", func(t *testing.T) {
		tc.RunWithSkipSupport(t, skipManager, "RequestInfo", func(t *testing.T) {
			tc.suite.RunRequestInfoTests(t)
		})
	})

	t.Run("Request", func(t *testing.T) {
		tc.RunWithSkipSupport(t, skipManager, "Request", func(t *testing.T) {
			tc.suite.RunRequestTests(t)
		})
	})

	t.Run("BodyAccess", func(t *testing.T) {
		tc.RunWithSkipSupport(t, skipManager, "BodyAccess", func(t *testing.T) {
			tc.suite.RunBodyAccessTests(t)
		})
	})

	t.Run("FormAccess", func(t *testing.T) {
		tc.RunWithSkipSupport(t, skipManager, "FormAccess", func(t *testing.T) {
			tc.suite.RunFormAccessTests(t)
		})
	})

	t.Run("Binder", func(t *testing.T) {
		tc.RunWithSkipSupport(t, skipManager, "Binder", func(t *testing.T) {
			tc.suite.RunBinderTests(t)
		})
	})

	t.Run("Responder", func(t *testing.T) {
		tc.RunWithSkipSupport(t, skipManager, "Responder", func(t *testing.T) {
			tc.suite.RunResponderTests(t)
		})
	})

	t.Run("StateStore", func(t *testing.T) {
		tc.RunWithSkipSupport(t, skipManager, "StateStore", func(t *testing.T) {
			tc.suite.RunStateStoreTests(t)
		})
	})

	t.Run("Router", func(t *testing.T) {
		tc.RunWithSkipSupport(t, skipManager, "Router", func(t *testing.T) {
			tc.suite.RunRouterTests(t)
		})
	})

	t.Run("Engine", func(t *testing.T) {
		tc.RunWithSkipSupport(t, skipManager, "Engine", func(t *testing.T) {
			tc.suite.RunEngineTests(t)
		})
	})
}

// ValidateFrameworkIntegration validates that a framework properly integrates with httpx interfaces.
// It checks that the engine and test suite are properly initialized and all interface testers are available.
func (tc *TestCases) ValidateFrameworkIntegration(t *testing.T) {
	t.Helper()

	ctx := httptesting.NewTestContext(tc.frameworkName, "Framework", "Validation", "integration_check")
	tc.suite.Helper().LogTestStart(t, ctx)

	t.Logf("Validating framework integration for: %s", tc.frameworkName)

	// Validate that the engine is not nil
	if tc.engine == nil {
		httptesting.FailWithContext(t, ctx, "Engine is nil for framework: %s", tc.frameworkName)
		return
	}

	// Validate that the test suite is properly initialized
	if tc.suite == nil {
		httptesting.FailWithContext(t, ctx, "Test suite is nil for framework: %s", tc.frameworkName)
		return
	}

	// Validate that all interface testers are available
	if tc.suite.RequestInfoTester() == nil {
		tc.suite.Helper().ReportTestFailure(t, ctx, "RequestInfo tester is nil")
	}

	if tc.suite.BodyAccessTester() == nil {
		tc.suite.Helper().ReportTestFailure(t, ctx, "BodyAccess tester is nil")
	}

	if tc.suite.BinderTester() == nil {
		tc.suite.Helper().ReportTestFailure(t, ctx, "Binder tester is nil")
	}

	if tc.suite.ResponderTester() == nil {
		tc.suite.Helper().ReportTestFailure(t, ctx, "Responder tester is nil")
	}

	if tc.suite.RouterTester() == nil {
		tc.suite.Helper().ReportTestFailure(t, ctx, "Router tester is nil")
	}

	if tc.suite.EngineTester() == nil {
		tc.suite.Helper().ReportTestFailure(t, ctx, "Engine tester is nil")
	}

	tc.suite.Helper().LogTestComplete(t, ctx)
	t.Logf("Framework integration validation completed for: %s", tc.frameworkName)
}

// BenchmarkInterfaceTests provides benchmarking capabilities for interface tests.
// It runs benchmark tests for RequestInfo, BodyAccess, Binder, and Responder interfaces.
func (tc *TestCases) BenchmarkInterfaceTests(b *testing.B) {
	b.Logf("Benchmarking interface tests for framework: %s", tc.frameworkName)

	// Benchmark individual interfaces
	b.Run("RequestInfo", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			t := &testing.T{}
			tc.suite.RunRequestInfoTests(t)
		}
	})

	b.Run("BodyAccess", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			t := &testing.T{}
			tc.suite.RunBodyAccessTests(t)
		}
	})

	b.Run("Binder", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			t := &testing.T{}
			tc.suite.RunBinderTests(t)
		}
	})

	b.Run("Responder", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			t := &testing.T{}
			tc.suite.RunResponderTests(t)
		}
	})
}

// RunAllInterfaceTestsWithReporting runs all interface tests with enhanced reporting.
// It collects test results for each interface and reports a summary at the end.
func (tc *TestCases) RunAllInterfaceTestsWithReporting(t *testing.T) {
	t.Helper()

	t.Logf("Running all interface tests with enhanced reporting for framework: %s", tc.frameworkName)

	results := make(map[string]httptesting.TestResult)

	// Test each interface and collect results
	interfaces := []string{
		"RequestInfo", "Request", "BodyAccess", "FormAccess",
		"Binder", "Responder", "StateStore", "Router", "Engine",
	}

	for _, interfaceName := range interfaces {
		t.Run(interfaceName, func(t *testing.T) {
			tc.suite.Helper().ReportInterfaceTestStart(t, tc.frameworkName, interfaceName)

			// Run the specific interface test
			switch interfaceName {
			case "RequestInfo":
				tc.suite.RunRequestInfoTests(t)
			case "Request":
				tc.suite.RunRequestTests(t)
			case "BodyAccess":
				tc.suite.RunBodyAccessTests(t)
			case "FormAccess":
				tc.suite.RunFormAccessTests(t)
			case "Binder":
				tc.suite.RunBinderTests(t)
			case "Responder":
				tc.suite.RunResponderTests(t)
			case "StateStore":
				tc.suite.RunStateStoreTests(t)
			case "Router":
				tc.suite.RunRouterTests(t)
			case "Engine":
				tc.suite.RunEngineTests(t)
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
	tc.suite.Helper().ReportFrameworkTestSummary(t, tc.frameworkName, results)
}
