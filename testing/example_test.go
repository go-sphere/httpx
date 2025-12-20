package testing

import (
	"fmt"
	"testing"
)

// ExampleTestSuite demonstrates how to use the comprehensive test suite
// with a mock adapter implementation.
func ExampleTestSuite() {
	// Create a mock engine (in real usage, this would be a concrete adapter like ginx, fiberx, etc.)
	mockEngine := &MockEngine{}
	
	// Create the test suite
	suite := NewTestSuite("example-adapter", mockEngine)
	
	// In a real test, you would call:
	// suite.RunAllTests(t)
	// suite.RunConcurrencyTests(t)
	// suite.RunBenchmarks(b)
	
	// Generate a report
	results := &TestResults{
		TotalTests:   100,
		PassedTests:  98,
		FailedTests:  2,
		SkippedTests: 0,
	}
	
	report := suite.GenerateReport(results)
	_ = report // In real usage, you would log or save this report
	
	// Output: Test suite created successfully
	fmt.Println("Test suite created successfully")
}

// TestExampleUsage shows how the TestSuite integrates all testing tools.
func TestExampleUsage(t *testing.T) {
	mockEngine := &MockEngine{}
	suite := NewTestSuite("integration-test", mockEngine)
	
	// Verify that all testers are properly initialized and integrated
	if suite.abortTracker == nil {
		t.Error("AbortTracker not integrated")
	}
	
	if suite.requestTester == nil {
		t.Error("RequestTester not integrated")
	}
	
	if suite.binderTester == nil {
		t.Error("BinderTester not integrated")
	}
	
	if suite.responderTester == nil {
		t.Error("ResponderTester not integrated")
	}
	
	if suite.stateStoreTester == nil {
		t.Error("StateStoreTester not integrated")
	}
	
	if suite.routerTester == nil {
		t.Error("RouterTester not integrated")
	}
	
	if suite.engineTester == nil {
		t.Error("EngineTester not integrated")
	}
	
	// Test that the suite can generate reports
	results := &TestResults{
		TotalTests:   10,
		PassedTests:  10,
		FailedTests:  0,
		SkippedTests: 0,
	}
	
	report := suite.GenerateReport(results)
	if report == "" {
		t.Error("Report generation failed")
	}
	
	if !contains(report, "integration-test") {
		t.Error("Report should contain adapter name")
	}
	
	t.Log("TestSuite integration test completed successfully")
}