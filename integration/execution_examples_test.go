package integration

import (
	"testing"

	httptesting "github.com/go-sphere/httpx/testing"
)

// TestExecutionExamples demonstrates various ways to execute tests flexibly
func TestExecutionExamples(t *testing.T) {
	// Example 1: Run all frameworks in batch mode (comprehensive check)
	t.Run("Example1_QuickValidation", func(t *testing.T) {
		runner := NewTestRunner()
		runner.RunAllFrameworks(t, ModeBatch)
	})

	// Example 2: Run specific frameworks with custom config
	t.Run("Example2_CustomConfig", func(t *testing.T) {
		config := &httptesting.TestConfig{
			ServerAddr:     ":0",
			VerboseLogging: true,
			SkipSlowTests:  true,
		}

		runner := NewTestRunnerWithConfig(config)
		frameworks := []FrameworkType{FrameworkGinx, FrameworkFiberx}
		runner.RunSpecificFrameworks(t, frameworks, ModeIndividual)
	})

	// Example 3: Test specific interface across all frameworks
	t.Run("Example3_InterfaceComparison", func(t *testing.T) {
		runner := NewTestRunner()

		// Test RequestInfo across all frameworks
		runner.RunInterfaceAcrossFrameworks(t, "RequestInfo")

		// Test Binder across all frameworks
		runner.RunInterfaceAcrossFrameworks(t, "Binder")
	})

	// Example 4: Options-based execution
	t.Run("Example4_OptionsExecution", func(t *testing.T) {
		runner := NewTestRunner()

		options := TestExecutionOptions{
			Frameworks: []FrameworkType{FrameworkGinx, FrameworkEchox},
			Mode:       ModeIndividual,
			Interfaces: []string{"RequestInfo", "Responder", "Router"},
		}

		runner.RunWithOptions(t, options)
	})

	// Example 5: Performance comparison
	t.Run("Example5_PerformanceComparison", func(t *testing.T) {
		runner := NewTestRunner()

		// Compare performance of different interfaces
		interfaces := []string{"RequestInfo", "Binder", "Responder"}

		for _, interfaceName := range interfaces {
			t.Run(interfaceName, func(t *testing.T) {
				results := runner.CompareFrameworkPerformance(t, interfaceName)

				// Find fastest and slowest
				var fastest, slowest FrameworkType
				var fastestTime, slowestTime = results[FrameworkGinx].Duration, results[FrameworkGinx].Duration

				for framework, metrics := range results {
					if metrics.Duration < fastestTime {
						fastest = framework
						fastestTime = metrics.Duration
					}
					if metrics.Duration > slowestTime {
						slowest = framework
						slowestTime = metrics.Duration
					}
				}

				t.Logf("%s interface - Fastest: %s (%v), Slowest: %s (%v)",
					interfaceName, fastest, fastestTime, slowest, slowestTime)
			})
		}
	})
}

// TestBatchVsIndividualComparison compares batch vs individual execution modes
func TestBatchVsIndividualComparison(t *testing.T) {
	runner := NewTestRunner()

	// Test ginx in both modes and compare
	t.Run("GinxBatchVsIndividual", func(t *testing.T) {
		t.Run("BatchMode", func(t *testing.T) {
			runner.RunSingleFramework(t, FrameworkGinx, ModeBatch)
		})

		t.Run("IndividualMode", func(t *testing.T) {
			runner.RunSingleFramework(t, FrameworkGinx, ModeIndividual)
		})
	})
}

// TestFrameworkSpecificExecution demonstrates framework-specific test execution
func TestFrameworkSpecificExecution(t *testing.T) {
	runner := NewTestRunner()

	// Test each framework individually
	frameworks := runner.GetAvailableFrameworks()

	for _, framework := range frameworks {
		t.Run(string(framework), func(t *testing.T) {
			// Run batch test first
			t.Run("Validation", func(t *testing.T) {
				runner.RunSingleFramework(t, framework, ModeBatch)
			})

			// Then run a subset of interfaces
			interfaces := []string{"RequestInfo", "Binder"}
			for _, interfaceName := range interfaces {
				t.Run(interfaceName, func(t *testing.T) {
					engine, _ := runner.GetFrameworkEngine(framework)
					skipMgr, _ := runner.GetFrameworkSkipManager(framework)
					tc := NewTestCases(string(framework), engine)

					tc.RunWithSkipSupport(t, skipMgr, interfaceName, func(t *testing.T) {
						tc.RunSpecificInterfaceTest(t, interfaceName)
					})
				})
			}
		})
	}
}

// TestExecutionFlexibilityFeatures tests all flexibility features
func TestExecutionFlexibilityFeatures(t *testing.T) {
	runner := NewTestRunner()

	// Feature 1: Framework availability checking
	t.Run("FrameworkAvailability", func(t *testing.T) {
		frameworks := runner.GetAvailableFrameworks()
		t.Logf("Available frameworks: %v", frameworks)

		if len(frameworks) == 0 {
			t.Error("No frameworks available")
		}

		// Check each framework
		for _, framework := range frameworks {
			engine, exists := runner.GetFrameworkEngine(framework)
			if !exists {
				t.Errorf("Framework %s should be available", framework)
			}
			if engine == nil {
				t.Errorf("Engine for %s should not be nil", framework)
			}
		}
	})

	// Feature 2: Skip manager configuration
	t.Run("SkipManagerConfiguration", func(t *testing.T) {
		frameworks := runner.GetAvailableFrameworks()

		for _, framework := range frameworks {
			skipMgr, exists := runner.GetFrameworkSkipManager(framework)
			if !exists {
				t.Errorf("Skip manager for %s should be available", framework)
				continue
			}

			skippedTests := skipMgr.GetSkippedTests(string(framework))
			t.Logf("Framework %s has %d skipped tests", framework, len(skippedTests))
		}
	})

	// Feature 3: Custom configuration support
	t.Run("CustomConfiguration", func(t *testing.T) {
		customConfig := &httptesting.TestConfig{
			ServerAddr:     ":0",
			VerboseLogging: true,
			SkipSlowTests:  true,
			MaxRetries:     5,
		}

		customRunner := NewTestRunnerWithConfig(customConfig)

		// Run a quick validation with custom config
		customRunner.RunSingleFramework(t, FrameworkGinx, ModeBatch)
	})

	// Feature 4: Summary reporting
	t.Run("SummaryReporting", func(t *testing.T) {
		runner.PrintFrameworkSummary(t)
	})
}

// BenchmarkExecutionFlexibility benchmarks the flexible execution system
func BenchmarkExecutionFlexibility(b *testing.B) {
	runner := NewTestRunner()

	// Benchmark single framework execution
	b.Run("SingleFramework", func(b *testing.B) {
		runner.BenchmarkSingleFramework(b, FrameworkGinx)
	})

	// Benchmark all frameworks
	b.Run("AllFrameworks", func(b *testing.B) {
		runner.BenchmarkAllFrameworks(b)
	})

	// Benchmark framework comparison
	b.Run("FrameworkComparison", func(b *testing.B) {
		runner.BenchmarkComparison(b)
	})
}

// TestExecutionModeValidation tests that all execution modes work correctly
func TestExecutionModeValidation(t *testing.T) {
	runner := NewTestRunner()

	modes := []TestExecutionMode{
		ModeIndividual,
		ModeBatch,
	}

	// Test each mode with ginx (reference implementation)
	for _, mode := range modes {
		t.Run(string(mode), func(t *testing.T) {
			runner.RunSingleFramework(t, FrameworkGinx, mode)
		})
	}

	// Test invalid mode handling
	t.Run("InvalidMode", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Error("Expected panic for invalid mode")
			}
		}()

		// This should panic for invalid mode
		runner.RunSingleFramework(t, FrameworkGinx, TestExecutionMode("invalid"))
	})
}

// TestConcurrentExecution tests concurrent execution capabilities
func TestConcurrentExecution(t *testing.T) {
	runner := NewTestRunner()

	// Run multiple frameworks concurrently (Go's testing framework handles this)
	t.Run("ConcurrentFrameworks", func(t *testing.T) {
		frameworks := []FrameworkType{FrameworkGinx, FrameworkFiberx, FrameworkEchox}

		for _, framework := range frameworks {
			framework := framework // Capture loop variable
			t.Run(string(framework), func(t *testing.T) {
				t.Parallel() // Enable parallel execution
				runner.RunSingleFramework(t, framework, ModeBatch)
			})
		}
	})
}
