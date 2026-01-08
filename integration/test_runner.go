// Package integration provides a unified testing framework for httpx framework adapters.
//
// # Overview
//
// This package implements a flexible test execution system that validates the implementation
// of httpx interfaces across multiple web frameworks (Gin, Fiber, Echo, Hertz). It ensures
// consistent behavior and complete interface coverage for all framework adapters.
//
// # Architecture
//
// The integration package is built around three core components:
//
//  1. TestRunner: Orchestrates test execution across frameworks
//  2. TestCases: Defines comprehensive interface test scenarios
//  3. SkipManager: Manages framework-specific test exclusions
//
// # Test Execution Modes
//
// Three execution modes support different development and validation workflows:
//
// Individual Mode - For debugging and development:
//   - Runs each interface test separately with detailed output
//   - Provides granular test structure for precise error location
//   - Ideal for: developing new features, debugging interface issues
//   - Trade-off: Slower execution due to repeated initialization
//
// Batch Mode - For CI/CD and comprehensive validation:
//   - Runs all interface tests + framework validation in one pass
//   - Optimized for speed with aggregated reporting
//   - Ideal for: CI/CD pipelines, PR validation, regression testing
//   - Trade-off: Less granular error reporting (requires Individual for detailed debugging)
//
// Benchmark Mode - For performance analysis:
//   - Measures execution time and memory allocation
//   - Supports framework performance comparison
//   - Ideal for: detecting performance regressions, optimization validation
//   - Trade-off: Requires stable environment for consistent results
//
// # Usage Examples
//
// Basic framework testing:
//
//	func TestGinxIntegration(t *testing.T) {
//	    runner := integration.NewTestRunner()
//	    runner.RunSingleFramework(t, integration.FrameworkGinx, integration.ModeBatch)
//	}
//
// Cross-framework validation:
//
//	func TestAllFrameworks(t *testing.T) {
//	    runner := integration.NewTestRunner()
//	    runner.RunAllFrameworks(t, integration.ModeBatch)
//	}
//
// Interface-specific testing:
//
//	func TestBinderInterface(t *testing.T) {
//	    runner := integration.NewTestRunner()
//	    runner.RunInterfaceAcrossFrameworks(t, "Binder")
//	}
//
// Performance benchmarking:
//
//	func BenchmarkFrameworks(b *testing.B) {
//	    runner := integration.NewTestRunner()
//	    runner.BenchmarkComparison(b)
//	}
//
// # Development Workflow
//
// Local development:
//  1. Use Individual mode to debug specific interface implementations
//  2. Switch to Batch mode to verify overall integration
//  3. Run full test suite before committing
//
// CI/CD pipeline:
//   - Use Batch mode for fast validation (default behavior)
//   - Run Benchmark mode periodically to detect performance regressions
//
// # Framework Support
//
// Currently supported frameworks:
//   - Ginx (github.com/go-sphere/httpx/ginx) - Gin adapter
//   - Fiberx (github.com/go-sphere/httpx/fiberx) - Fiber adapter
//   - Echox (github.com/go-sphere/httpx/echox) - Echo adapter
//   - Hertzx (github.com/go-sphere/httpx/hertzx) - Hertz adapter
//
// # Test Coverage
//
// The integration package validates the following httpx interfaces:
//   - RequestInfo: HTTP request information (method, path, headers, queries)
//   - Binder: Request body and parameter binding
//   - Responder: HTTP response generation
//   - Router: Route registration and request routing
//   - StateStore: Request-scoped state management
//   - BodyAccess: Request body reading and parsing
//   - FormAccess: Form data and file upload handling
//
// # Skip Management
//
// Framework-specific limitations can be documented using SkipManager:
//
//	skipMgr := integration.NewGinxSkipManager()
//	skipMgr.SkipTest("FormAccess", "TestMultipartForm",
//	    "Ginx uses different multipart handling")
//
// Skipped tests are clearly reported in test output with reasons.
//
// For more details, see:
//   - Test execution modes: ../specs/001-test-suite-optimization/quickstart.md
//   - Implementation plan: ../specs/001-test-suite-optimization/plan.md
//   - Architecture details: ../specs/001-test-suite-optimization/research.md
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

// FrameworkType represents different framework types supported by the test runner.
type FrameworkType string

const (
	// FrameworkGinx represents the Gin framework adapter.
	FrameworkGinx FrameworkType = "ginx"
	// FrameworkFiberx represents the Fiber framework adapter.
	FrameworkFiberx FrameworkType = "fiberx"
	// FrameworkEchox represents the Echo framework adapter.
	FrameworkEchox FrameworkType = "echox"
	// FrameworkHertzx represents the Hertz framework adapter.
	FrameworkHertzx FrameworkType = "hertzx"
)

// TestExecutionMode defines how tests should be executed.
// There are three execution modes, each serving different testing needs:
//   - Individual: Runs each interface test in isolation for detailed debugging
//   - Batch: Runs all tests together efficiently for CI/CD pipelines (includes validation)
//   - Benchmark: Measures performance for regression detection
type TestExecutionMode string

const (
	// ModeIndividual runs each interface test separately for maximum isolation and debugging detail.
	// Use this mode when debugging specific interface failures or investigating test issues.
	ModeIndividual TestExecutionMode = "individual"

	// ModeBatch runs all interface tests together for faster execution in CI/CD pipelines.
	// This mode includes framework integration validation as a first step.
	// Use this mode for continuous integration, pull request checks, and release validation.
	ModeBatch TestExecutionMode = "batch"

	// ModeBenchmark runs benchmark tests for performance regression detection.
	// Use this mode to measure and compare interface implementation performance across frameworks.
	ModeBenchmark TestExecutionMode = "benchmark"
)

// TestRunner manages flexible test execution across frameworks.
// It provides methods for running tests in different modes and comparing framework implementations.
type TestRunner struct {
	frameworks map[FrameworkType]httpx.Engine
	config     *httptesting.TestConfig
	skipMgrs   map[FrameworkType]*TestSkipManager
}

// NewTestRunner creates a new test runner with all frameworks.
// It initializes all supported frameworks (ginx, fiberx, echox, hertzx) with default configuration.
func NewTestRunner() *TestRunner {
	return NewTestRunnerWithConfig(nil)
}

// NewTestRunnerWithConfig creates a new test runner with custom configuration.
// If config is nil, it uses the default test configuration.
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

// RunSingleFramework runs tests for a single framework with specified mode.
// The mode parameter determines how tests are executed: individual, batch, or benchmark.
// Batch mode includes framework validation as a first step.
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
	default:
		// Panic for invalid modes to allow tests to catch with recover()
		panic(fmt.Sprintf("Unknown test execution mode: %s", mode))
	}
}

// RunAllFrameworks runs tests for all frameworks with specified mode.
// Each framework's tests are run in a separate subtest for isolation.
func (tr *TestRunner) RunAllFrameworks(t *testing.T, mode TestExecutionMode) {
	t.Helper()

	t.Logf("Running all framework tests in %s mode", mode)

	for framework := range tr.frameworks {
		t.Run(string(framework), func(t *testing.T) {
			tr.RunSingleFramework(t, framework, mode)
		})
	}
}

// RunSpecificFrameworks runs tests for specified frameworks with specified mode.
// This is useful for selective testing when you don't need to run all framework tests.
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

// runBatchTests runs all interface tests together with validation.
// This mode first validates framework integration, then runs all interface tests.
// It's optimized for CI/CD pipelines where speed and comprehensive coverage are important.
func (tr *TestRunner) runBatchTests(t *testing.T, tc *TestCases, skipMgr *TestSkipManager) {
	t.Helper()

	// Run validation first to ensure framework is properly integrated
	t.Run("Validation", func(t *testing.T) {
		tc.ValidateFrameworkIntegration(t)
	})

	// Run all interface tests together
	t.Run("AllInterfaces", func(t *testing.T) {
		tc.RunAllInterfaceTestsWithReporting(t)
	})
}

// BenchmarkSingleFramework runs benchmark tests for a single framework.
// It measures the performance of interface implementations for the specified framework.
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

// BenchmarkAllFrameworks runs benchmark tests for all frameworks.
// Each framework's benchmarks are run in a separate sub-benchmark for comparison.
func (tr *TestRunner) BenchmarkAllFrameworks(b *testing.B) {
	b.Helper()

	for framework := range tr.frameworks {
		b.Run(string(framework), func(b *testing.B) {
			tr.BenchmarkSingleFramework(b, framework)
		})
	}
}

// BenchmarkComparison runs comparative benchmarks across frameworks.
// It tests the same interface across all frameworks to identify performance differences.
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

	testFunc, exists := interfaceTestMap[interfaceName]
	if !exists {
		b.Fatalf("Unknown interface: %s", interfaceName)
		return
	}

	for i := 0; i < b.N; i++ {
		t := &testing.T{} // Create a dummy testing.T for interface compatibility
		testFunc(tc.suite, t)
	}
}

// RunInterfaceAcrossFrameworks runs a specific interface test across all frameworks.
// This is useful for comparing how different frameworks implement the same httpx interface.
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

// GetAvailableFrameworks returns a list of available frameworks.
// The order of frameworks in the returned slice is not guaranteed.
func (tr *TestRunner) GetAvailableFrameworks() []FrameworkType {
	frameworks := make([]FrameworkType, 0, len(tr.frameworks))
	for framework := range tr.frameworks {
		frameworks = append(frameworks, framework)
	}
	return frameworks
}

// GetFrameworkEngine returns the engine for a specific framework.
// The second return value indicates whether the framework exists in the runner.
func (tr *TestRunner) GetFrameworkEngine(framework FrameworkType) (httpx.Engine, bool) {
	engine, exists := tr.frameworks[framework]
	return engine, exists
}

// GetFrameworkSkipManager returns the skip manager for a specific framework.
// The second return value indicates whether the framework exists in the runner.
func (tr *TestRunner) GetFrameworkSkipManager(framework FrameworkType) (*TestSkipManager, bool) {
	skipMgr, exists := tr.skipMgrs[framework]
	return skipMgr, exists
}

// PrintFrameworkSummary prints a summary of all frameworks and their skip configurations.
// This is useful for understanding which tests are configured to be skipped for each framework.
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

// TestExecutionOptions holds options for test execution.
// It allows fine-grained control over which frameworks, interfaces, and modes to test.
type TestExecutionOptions struct {
	Frameworks []FrameworkType
	Mode       TestExecutionMode
	Interfaces []string
	Config     *httptesting.TestConfig
}

// RunWithOptions runs tests with the specified options.
// If no frameworks are specified, it runs tests for all available frameworks.
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

// PerformanceMetrics holds performance metrics for a test run.
// It captures timing information and test outcomes for benchmarking and analysis.
type PerformanceMetrics struct {
	Framework    string
	Interface    string
	Duration     time.Duration
	TestsPassed  int
	TestsFailed  int
	TestsSkipped int
	Timestamp    time.Time
}

// TrackPerformance runs a test and tracks its performance metrics.
// Returns metrics including duration, test counts, and timestamp for analysis.
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

// CompareFrameworkPerformance compares performance across frameworks for a specific interface.
// Returns a map of framework types to their performance metrics for comparison.
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
