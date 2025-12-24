package integration

import (
	"testing"
)

// TestFlexibleExecution demonstrates flexible test execution capabilities
func TestFlexibleExecution(t *testing.T) {
	runner := NewTestRunner()

	// Print framework summary
	t.Run("FrameworkSummary", func(t *testing.T) {
		runner.PrintFrameworkSummary(t)
	})
}

// TestSingleFrameworkExecution tests running a single framework with different modes
func TestSingleFrameworkExecution(t *testing.T) {
	runner := NewTestRunner()

	// Test individual mode
	t.Run("GinxIndividual", func(t *testing.T) {
		runner.RunSingleFramework(t, FrameworkGinx, ModeIndividual)
	})

	// Test batch mode
	t.Run("GinxBatch", func(t *testing.T) {
		runner.RunSingleFramework(t, FrameworkGinx, ModeBatch)
	})

	// Test validation mode
	t.Run("GinxValidation", func(t *testing.T) {
		runner.RunSingleFramework(t, FrameworkGinx, ModeValidation)
	})
}

// TestMultipleFrameworkExecution tests running multiple frameworks
func TestMultipleFrameworkExecution(t *testing.T) {
	runner := NewTestRunner()

	// Test specific frameworks
	frameworks := []FrameworkType{FrameworkGinx, FrameworkFiberx}

	t.Run("SpecificFrameworksIndividual", func(t *testing.T) {
		runner.RunSpecificFrameworks(t, frameworks, ModeIndividual)
	})

	t.Run("SpecificFrameworksBatch", func(t *testing.T) {
		runner.RunSpecificFrameworks(t, frameworks, ModeBatch)
	})
}

// TestAllFrameworkExecution tests running all frameworks
func TestAllFrameworkExecution(t *testing.T) {
	runner := NewTestRunner()

	t.Run("AllFrameworksValidation", func(t *testing.T) {
		runner.RunAllFrameworks(t, ModeValidation)
	})

	// Uncomment to run full tests (may be slow)
	// t.Run("AllFrameworksIndividual", func(t *testing.T) {
	//     runner.RunAllFrameworks(t, ModeIndividual)
	// })
}

// TestInterfaceAcrossFrameworks tests running a specific interface across all frameworks
func TestInterfaceAcrossFrameworks(t *testing.T) {
	runner := NewTestRunner()

	// Test RequestInfo interface across all frameworks
	t.Run("RequestInfoAcrossFrameworks", func(t *testing.T) {
		runner.RunInterfaceAcrossFrameworks(t, "RequestInfo")
	})

	// Test Binder interface across all frameworks
	t.Run("BinderAcrossFrameworks", func(t *testing.T) {
		runner.RunInterfaceAcrossFrameworks(t, "Binder")
	})
}

// TestWithOptions demonstrates using the options-based execution
func TestWithOptions(t *testing.T) {
	runner := NewTestRunner()

	// Test specific frameworks and interfaces
	options := TestExecutionOptions{
		Frameworks: []FrameworkType{FrameworkGinx, FrameworkFiberx},
		Mode:       ModeIndividual,
		Interfaces: []string{"RequestInfo", "Binder"},
	}

	t.Run("OptionsBasedExecution", func(t *testing.T) {
		runner.RunWithOptions(t, options)
	})
}

// TestPerformanceComparison demonstrates performance comparison capabilities
func TestPerformanceComparison(t *testing.T) {
	runner := NewTestRunner()

	// Compare RequestInfo performance across frameworks
	t.Run("RequestInfoPerformanceComparison", func(t *testing.T) {
		results := runner.CompareFrameworkPerformance(t, "RequestInfo")

		// Log results
		for framework, metrics := range results {
			t.Logf("Framework %s: Duration=%v, Passed=%d, Failed=%d",
				framework, metrics.Duration, metrics.TestsPassed, metrics.TestsFailed)
		}
	})
}

// BenchmarkFlexibleExecution provides benchmark tests using the flexible execution system
func BenchmarkFlexibleExecution(b *testing.B) {
	runner := NewTestRunner()

	// Benchmark single framework
	b.Run("GinxBenchmark", func(b *testing.B) {
		runner.BenchmarkSingleFramework(b, FrameworkGinx)
	})

	// Benchmark all frameworks
	b.Run("AllFrameworksBenchmark", func(b *testing.B) {
		runner.BenchmarkAllFrameworks(b)
	})

	// Benchmark comparison
	b.Run("FrameworkComparison", func(b *testing.B) {
		runner.BenchmarkComparison(b)
	})
}

// TestFrameworkAvailability tests framework availability checking
func TestFrameworkAvailability(t *testing.T) {
	runner := NewTestRunner()

	// Check available frameworks
	frameworks := runner.GetAvailableFrameworks()
	t.Logf("Available frameworks: %v", frameworks)

	// Check specific framework engine
	engine, exists := runner.GetFrameworkEngine(FrameworkGinx)
	if !exists {
		t.Error("Ginx framework should be available")
	}
	if engine == nil {
		t.Error("Ginx engine should not be nil")
	}

	// Check skip manager
	skipMgr, exists := runner.GetFrameworkSkipManager(FrameworkFiberx)
	if !exists {
		t.Error("Fiberx skip manager should be available")
	}
	if skipMgr == nil {
		t.Error("Fiberx skip manager should not be nil")
	}
}

// TestExecutionModes tests different execution modes
func TestExecutionModes(t *testing.T) {
	runner := NewTestRunner()

	modes := []TestExecutionMode{
		ModeIndividual,
		ModeBatch,
		ModeValidation,
	}

	for _, mode := range modes {
		t.Run(string(mode), func(t *testing.T) {
			// Test with ginx as it's the reference implementation
			runner.RunSingleFramework(t, FrameworkGinx, mode)
		})
	}
}
