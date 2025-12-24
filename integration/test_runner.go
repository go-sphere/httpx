package integration

import (
	"fmt"
	"testing"
	"time"

	"github.com/go-sphere/httpx"
	"github.com/go-sphere/httpx/echox"
	"github.com/go-sphere/httpx/fiberx"
	"github.com/go-sphere/httpx/ginx"
	"github.com/go-sphere/httpx/hertzx"
	httptesting "github.com/go-sphere/httpx/testing"
)

// FrameworkType represents different framework types
type FrameworkType string

const (
	FrameworkGinx   FrameworkType = "ginx"
	FrameworkFiberx FrameworkType = "fiberx"
	FrameworkEchox  FrameworkType = "echox"
	FrameworkHertzx FrameworkType = "hertzx"
)

// TestExecutionMode defines how tests should be executed
type TestExecutionMode string

const (
	ModeIndividual TestExecutionMode = "individual" // Run each interface test separately
	ModeBatch      TestExecutionMode = "batch"      // Run all interface tests together
	ModeBenchmark  TestExecutionMode = "benchmark"  // Run benchmark tests
	ModeValidation TestExecutionMode = "validation" // Run validation tests only
)

// TestRunner manages flexible test execution across frameworks
type TestRunner struct {
	frameworks map[FrameworkType]httpx.Engine
	config     *httptesting.TestConfig
	skipMgrs   map[FrameworkType]*TestSkipManager
}

// NewTestRunner creates a new test runner with all frameworks
func NewTestRunner() *TestRunner {
	return NewTestRunnerWithConfig(nil)
}

// NewTestRunnerWithConfig creates a new test runner with custom configuration
func NewTestRunnerWithConfig(config *httptesting.TestConfig) *TestRunner {
	if config == nil {
		config = httptesting.DefaultTestConfig()
	}

	tr := &TestRunner{
		frameworks: make(map[FrameworkType]httpx.Engine),
		config:     config,
		skipMgrs:   make(map[FrameworkType]*TestSkipManager),
	}

	// Initialize all frameworks
	tr.initializeFrameworks()
	tr.initializeSkipManagers()

	return tr
}

// initializeFrameworks sets up all framework engines
func (tr *TestRunner) initializeFrameworks() {
	tr.frameworks[FrameworkGinx] = ginx.New(ginx.WithServerAddr(":0"))
	tr.frameworks[FrameworkFiberx] = fiberx.New(fiberx.WithListen(":0"))
	tr.frameworks[FrameworkEchox] = echox.New(echox.WithServerAddr(":0"))
	tr.frameworks[FrameworkHertzx] = hertzx.New()
}

// initializeSkipManagers sets up skip managers for each framework
func (tr *TestRunner) initializeSkipManagers() {
	tr.skipMgrs[FrameworkGinx] = setupGinxSkipManager()
	tr.skipMgrs[FrameworkFiberx] = setupFiberxSkipManager()
	tr.skipMgrs[FrameworkEchox] = setupEchoxSkipManager()
	tr.skipMgrs[FrameworkHertzx] = setupHertzxSkipManager()
}

// RunSingleFramework runs tests for a single framework with specified mode
func (tr *TestRunner) RunSingleFramework(t *testing.T, framework FrameworkType, mode TestExecutionMode) {
	t.Helper()

	engine, exists := tr.frameworks[framework]
	if !exists {
		t.Fatalf("Framework %s not found", framework)
	}

	skipMgr := tr.skipMgrs[framework]
	tc := NewTestCasesWithConfig(string(framework), engine, tr.config)

	t.Logf("Running %s framework tests in %s mode", framework, mode)

	switch mode {
	case ModeIndividual:
		tr.runIndividualTests(t, tc, skipMgr)
	case ModeBatch:
		tr.runBatchTests(t, tc, skipMgr)
	case ModeBenchmark:
		// Benchmarks need to be run separately, log info for now
		t.Logf("Benchmark mode for %s - use BenchmarkSingleFramework method", framework)
	case ModeValidation:
		tr.runValidationTests(t, tc)
	default:
		// Panic for invalid modes to allow tests to catch with recover()
		panic(fmt.Sprintf("Unknown test execution mode: %s", mode))
	}
}

// RunAllFrameworks runs tests for all frameworks with specified mode
func (tr *TestRunner) RunAllFrameworks(t *testing.T, mode TestExecutionMode) {
	t.Helper()

	t.Logf("Running all framework tests in %s mode", mode)

	for framework := range tr.frameworks {
		t.Run(string(framework), func(t *testing.T) {
			tr.RunSingleFramework(t, framework, mode)
		})
	}
}

// RunSpecificFrameworks runs tests for specified frameworks with specified mode
func (tr *TestRunner) RunSpecificFrameworks(t *testing.T, frameworks []FrameworkType, mode TestExecutionMode) {
	t.Helper()

	t.Logf("Running tests for %d frameworks in %s mode", len(frameworks), mode)

	for _, framework := range frameworks {
		t.Run(string(framework), func(t *testing.T) {
			tr.RunSingleFramework(t, framework, mode)
		})
	}
}

// runIndividualTests runs each interface test individually
func (tr *TestRunner) runIndividualTests(t *testing.T, tc *TestCases, skipMgr *TestSkipManager) {
	t.Helper()
	tc.RunIndividualInterfaceTestsWithSkipSupport(t, skipMgr)
}

// runBatchTests runs all interface tests together
func (tr *TestRunner) runBatchTests(t *testing.T, tc *TestCases, skipMgr *TestSkipManager) {
	t.Helper()

	// Run validation first
	t.Run("Validation", func(t *testing.T) {
		tc.ValidateFrameworkIntegration(t)
	})

	// Run all tests together
	t.Run("AllInterfaces", func(t *testing.T) {
		tc.RunAllInterfaceTestsWithReporting(t)
	})
}

// runValidationTests runs only validation tests
func (tr *TestRunner) runValidationTests(t *testing.T, tc *TestCases) {
	t.Helper()
	tc.ValidateFrameworkIntegration(t)
}

// BenchmarkSingleFramework runs benchmark tests for a single framework
func (tr *TestRunner) BenchmarkSingleFramework(b *testing.B, framework FrameworkType) {
	b.Helper()

	engine, exists := tr.frameworks[framework]
	if !exists {
		b.Fatalf("Framework %s not found", framework)
	}

	tc := NewTestCasesWithConfig(string(framework), engine, tr.config)

	b.Logf("Running benchmark tests for framework: %s", framework)
	tc.BenchmarkInterfaceTests(b)
}

// BenchmarkAllFrameworks runs benchmark tests for all frameworks
func (tr *TestRunner) BenchmarkAllFrameworks(b *testing.B) {
	b.Helper()

	for framework := range tr.frameworks {
		b.Run(string(framework), func(b *testing.B) {
			tr.BenchmarkSingleFramework(b, framework)
		})
	}
}

// BenchmarkComparison runs comparative benchmarks across frameworks
func (tr *TestRunner) BenchmarkComparison(b *testing.B) {
	b.Helper()

	interfaces := []string{"RequestInfo", "BodyAccess", "Binder", "Responder"}

	for _, interfaceName := range interfaces {
		b.Run(interfaceName, func(b *testing.B) {
			for framework := range tr.frameworks {
				b.Run(string(framework), func(b *testing.B) {
					engine := tr.frameworks[framework]
					tc := NewTestCasesWithConfig(string(framework), engine, tr.config)

					// Run specific interface benchmark
					tr.benchmarkSpecificInterface(b, tc, interfaceName)
				})
			}
		})
	}
}

// benchmarkSpecificInterface runs benchmark for a specific interface
func (tr *TestRunner) benchmarkSpecificInterface(b *testing.B, tc *TestCases, interfaceName string) {
	b.Helper()

	for i := 0; i < b.N; i++ {
		t := &testing.T{} // Create a dummy testing.T for interface compatibility

		switch interfaceName {
		case "RequestInfo":
			tc.suite.RunRequestInfoTests(t)
		case "BodyAccess":
			tc.suite.RunBodyAccessTests(t)
		case "Binder":
			tc.suite.RunBinderTests(t)
		case "Responder":
			tc.suite.RunResponderTests(t)
		default:
			b.Fatalf("Unknown interface: %s", interfaceName)
		}
	}
}

// RunInterfaceAcrossFrameworks runs a specific interface test across all frameworks
func (tr *TestRunner) RunInterfaceAcrossFrameworks(t *testing.T, interfaceName string) {
	t.Helper()

	t.Logf("Running %s interface tests across all frameworks", interfaceName)

	for framework := range tr.frameworks {
		t.Run(string(framework), func(t *testing.T) {
			engine := tr.frameworks[framework]
			skipMgr := tr.skipMgrs[framework]
			tc := NewTestCasesWithConfig(string(framework), engine, tr.config)

			tc.RunWithSkipSupport(t, skipMgr, interfaceName, func(t *testing.T) {
				tc.RunSpecificInterfaceTest(t, interfaceName)
			})
		})
	}
}

// GetAvailableFrameworks returns a list of available frameworks
func (tr *TestRunner) GetAvailableFrameworks() []FrameworkType {
	frameworks := make([]FrameworkType, 0, len(tr.frameworks))
	for framework := range tr.frameworks {
		frameworks = append(frameworks, framework)
	}
	return frameworks
}

// GetFrameworkEngine returns the engine for a specific framework
func (tr *TestRunner) GetFrameworkEngine(framework FrameworkType) (httpx.Engine, bool) {
	engine, exists := tr.frameworks[framework]
	return engine, exists
}

// GetFrameworkSkipManager returns the skip manager for a specific framework
func (tr *TestRunner) GetFrameworkSkipManager(framework FrameworkType) (*TestSkipManager, bool) {
	skipMgr, exists := tr.skipMgrs[framework]
	return skipMgr, exists
}

// PrintFrameworkSummary prints a summary of all frameworks and their skip configurations
func (tr *TestRunner) PrintFrameworkSummary(t *testing.T) {
	t.Helper()

	t.Log("Framework Test Runner Summary:")
	t.Logf("  Total Frameworks: %d", len(tr.frameworks))
	t.Logf("  Configuration: %+v", tr.config)

	for framework, skipMgr := range tr.skipMgrs {
		skippedTests := skipMgr.GetSkippedTests(string(framework))
		t.Logf("  %s: %d skipped tests configured", framework, len(skippedTests))

		for _, test := range skippedTests {
			t.Logf("    - %s.%s: %s", test.Interface, test.Method, test.Reason)
		}
	}
}

// TestExecutionOptions holds options for test execution
type TestExecutionOptions struct {
	Frameworks []FrameworkType
	Mode       TestExecutionMode
	Interfaces []string
	Config     *httptesting.TestConfig
}

// RunWithOptions runs tests with the specified options
func (tr *TestRunner) RunWithOptions(t *testing.T, options TestExecutionOptions) {
	t.Helper()

	if len(options.Frameworks) == 0 {
		options.Frameworks = tr.GetAvailableFrameworks()
	}

	if len(options.Interfaces) == 0 {
		// Run all interfaces
		tr.RunSpecificFrameworks(t, options.Frameworks, options.Mode)
	} else {
		// Run specific interfaces
		for _, interfaceName := range options.Interfaces {
			t.Run(interfaceName, func(t *testing.T) {
				for _, framework := range options.Frameworks {
					t.Run(string(framework), func(t *testing.T) {
						engine := tr.frameworks[framework]
						skipMgr := tr.skipMgrs[framework]
						tc := NewTestCasesWithConfig(string(framework), engine, tr.config)

						tc.RunWithSkipSupport(t, skipMgr, interfaceName, func(t *testing.T) {
							tc.RunSpecificInterfaceTest(t, interfaceName)
						})
					})
				}
			})
		}
	}
}

// Performance tracking utilities

// PerformanceMetrics holds performance metrics for a test run
type PerformanceMetrics struct {
	Framework    string
	Interface    string
	Duration     time.Duration
	TestsPassed  int
	TestsFailed  int
	TestsSkipped int
	Timestamp    time.Time
}

// TrackPerformance runs a test and tracks its performance metrics
func (tr *TestRunner) TrackPerformance(t *testing.T, framework FrameworkType, interfaceName string, testFunc func(*testing.T)) *PerformanceMetrics {
	t.Helper()

	start := time.Now()

	// Run the test
	testFunc(t)

	duration := time.Since(start)

	return &PerformanceMetrics{
		Framework:    string(framework),
		Interface:    interfaceName,
		Duration:     duration,
		TestsPassed:  1, // Simplified - would need actual counting
		TestsFailed:  0,
		TestsSkipped: 0,
		Timestamp:    start,
	}
}

// CompareFrameworkPerformance compares performance across frameworks for a specific interface
func (tr *TestRunner) CompareFrameworkPerformance(t *testing.T, interfaceName string) map[FrameworkType]*PerformanceMetrics {
	t.Helper()

	results := make(map[FrameworkType]*PerformanceMetrics)

	for framework := range tr.frameworks {
		engine := tr.frameworks[framework]
		tc := NewTestCasesWithConfig(string(framework), engine, tr.config)

		metrics := tr.TrackPerformance(t, framework, interfaceName, func(t *testing.T) {
			tc.RunSpecificInterfaceTest(t, interfaceName)
		})

		results[framework] = metrics
	}

	// Log comparison results
	t.Logf("Performance comparison for %s interface:", interfaceName)
	for framework, metrics := range results {
		t.Logf("  %s: %v", framework, metrics.Duration)
	}

	return results
}

// RunAllInterfaceTests runs all interface tests using the new interface testers
func (tc *TestCases) RunAllInterfaceTests(t *testing.T) {
	t.Helper()

	t.Logf("Running all interface tests for framework: %s", tc.frameworkName)

	// Run the complete test suite which coordinates all interface testers
	tc.suite.RunAllTests(t)
}

// RunIndividualInterfaceTests runs each interface test individually for better isolation
func (tc *TestCases) RunIndividualInterfaceTests(t *testing.T) {
	t.Helper()

	t.Logf("Running individual interface tests for framework: %s", tc.frameworkName)

	// Test each interface individually
	t.Run("RequestInfo", func(t *testing.T) {
		tc.suite.RunRequestInfoTests(t)
	})

	t.Run("Request", func(t *testing.T) {
		tc.suite.RunRequestTests(t)
	})

	t.Run("BodyAccess", func(t *testing.T) {
		tc.suite.RunBodyAccessTests(t)
	})

	t.Run("FormAccess", func(t *testing.T) {
		tc.suite.RunFormAccessTests(t)
	})

	t.Run("Binder", func(t *testing.T) {
		tc.suite.RunBinderTests(t)
	})

	t.Run("Responder", func(t *testing.T) {
		tc.suite.RunResponderTests(t)
	})

	t.Run("StateStore", func(t *testing.T) {
		tc.suite.RunStateStoreTests(t)
	})

	t.Run("Router", func(t *testing.T) {
		tc.suite.RunRouterTests(t)
	})

	t.Run("Engine", func(t *testing.T) {
		tc.suite.RunEngineTests(t)
	})
}
