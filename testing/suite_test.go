package testing

import (
	"context"
	"io/fs"
	"testing"
	"time"

	"github.com/go-sphere/httpx"
)

// MockEngine is a minimal mock implementation of httpx.Engine for testing the TestSuite
type MockEngine struct {
	running bool
	addr    string
}

func (m *MockEngine) Start() error {
	m.running = true
	m.addr = ":8080"
	return nil
}

func (m *MockEngine) Stop(ctx context.Context) error {
	m.running = false
	return nil
}

func (m *MockEngine) IsRunning() bool {
	return m.running
}

func (m *MockEngine) Addr() string {
	return m.addr
}

func (m *MockEngine) Use(handlers ...httpx.Middleware) {
	// Mock implementation
}

func (m *MockEngine) Group(prefix string, handlers ...httpx.Middleware) httpx.Router {
	return &MockRouter{prefix: prefix}
}

// MockRouter is a minimal mock implementation of httpx.Router
type MockRouter struct {
	prefix string
}

func (m *MockRouter) GET(path string, handler httpx.Handler) {
	// Mock implementation
}

func (m *MockRouter) POST(path string, handler httpx.Handler) {
	// Mock implementation
}

func (m *MockRouter) PUT(path string, handler httpx.Handler) {
	// Mock implementation
}

func (m *MockRouter) DELETE(path string, handler httpx.Handler) {
	// Mock implementation
}

func (m *MockRouter) PATCH(path string, handler httpx.Handler) {
	// Mock implementation
}

func (m *MockRouter) HEAD(path string, handler httpx.Handler) {
	// Mock implementation
}

func (m *MockRouter) OPTIONS(path string, handler httpx.Handler) {
	// Mock implementation
}

func (m *MockRouter) Handle(method, path string, handler httpx.Handler) {
	// Mock implementation
}

func (m *MockRouter) Any(path string, handler httpx.Handler) {
	// Mock implementation
}

func (m *MockRouter) Use(handlers ...httpx.Middleware) {
	// Mock implementation
}

func (m *MockRouter) Group(prefix string, handlers ...httpx.Middleware) httpx.Router {
	return &MockRouter{prefix: m.prefix + prefix}
}

func (m *MockRouter) BasePath() string {
	return m.prefix
}

func (m *MockRouter) Static(path, root string) {
	// Mock implementation
}

func (m *MockRouter) StaticFS(path string, fs fs.FS) {
	// Mock implementation
}

// TestNewTestSuite tests the creation of a new test suite.
func TestNewTestSuite(t *testing.T) {
	mockEngine := &MockEngine{}
	suite := NewTestSuite("test-adapter", mockEngine)

	if suite == nil {
		t.Fatal("NewTestSuite returned nil")
	}

	if suite.name != "test-adapter" {
		t.Errorf("Expected name 'test-adapter', got '%s'", suite.name)
	}

	if suite.engine == nil {
		t.Error("Engine not properly assigned")
	}

	// Verify all testers are initialized
	if suite.abortTracker == nil {
		t.Error("AbortTracker not initialized")
	}

	if suite.requestTester == nil {
		t.Error("RequestTester not initialized")
	}

	if suite.binderTester == nil {
		t.Error("BinderTester not initialized")
	}

	if suite.responderTester == nil {
		t.Error("ResponderTester not initialized")
	}

	if suite.stateStoreTester == nil {
		t.Error("StateStoreTester not initialized")
	}

	if suite.routerTester == nil {
		t.Error("RouterTester not initialized")
	}

	if suite.engineTester == nil {
		t.Error("EngineTester not initialized")
	}
}

// TestNewTestSuiteWithConfig tests creating a test suite with custom configuration.
func TestNewTestSuiteWithConfig(t *testing.T) {
	mockEngine := &MockEngine{}
	customConfig := TestConfig{
		ServerAddr:      ":9090",
		RequestTimeout:  10 * time.Second,
		ConcurrentUsers: 20,
		TestDataSize:    2048,
	}

	suite := NewTestSuiteWithConfig("custom-adapter", mockEngine, customConfig)

	if suite == nil {
		t.Fatal("NewTestSuiteWithConfig returned nil")
	}

	if suite.name != "custom-adapter" {
		t.Errorf("Expected name 'custom-adapter', got '%s'", suite.name)
	}

	if suite.config.ServerAddr != ":9090" {
		t.Errorf("Expected ServerAddr ':9090', got '%s'", suite.config.ServerAddr)
	}

	if suite.config.ConcurrentUsers != 20 {
		t.Errorf("Expected ConcurrentUsers 20, got %d", suite.config.ConcurrentUsers)
	}
}

// TestTestResults tests the TestResults structure and methods.
func TestTestResults(t *testing.T) {
	results := &TestResults{
		TotalTests:   100,
		PassedTests:  95,
		FailedTests:  3,
		SkippedTests: 2,
		Duration:     5 * time.Second,
		InterfaceCoverage: map[string]float64{
			"Request":    100.0,
			"Responder":  98.5,
			"StateStore": 100.0,
		},
		BenchmarkResults: map[string]string{
			"BasicRequest": "1000 ns/op",
			"JSONResponse": "2000 ns/op",
		},
		Errors: []string{
			"Test failed: expected X, got Y",
			"Timeout in concurrent test",
		},
	}

	// Test SuccessRate calculation
	expectedRate := 95.0
	actualRate := results.SuccessRate()
	if actualRate != expectedRate {
		t.Errorf("Expected success rate %.2f%%, got %.2f%%", expectedRate, actualRate)
	}

	// Test with zero total tests
	emptyResults := &TestResults{}
	if emptyResults.SuccessRate() != 0.0 {
		t.Errorf("Expected 0%% success rate for empty results, got %.2f%%", emptyResults.SuccessRate())
	}
}

// TestGenerateReport tests the report generation functionality.
func TestGenerateReport(t *testing.T) {
	mockEngine := &MockEngine{}
	suite := NewTestSuite("test-adapter", mockEngine)

	results := &TestResults{
		TotalTests:   50,
		PassedTests:  48,
		FailedTests:  2,
		SkippedTests: 0,
		Duration:     3 * time.Second,
		InterfaceCoverage: map[string]float64{
			"Request":   100.0,
			"Responder": 95.0,
		},
		BenchmarkResults: map[string]string{
			"BasicRequest": "500 ns/op",
		},
		Errors: []string{
			"Test error example",
		},
	}

	report := suite.GenerateReport(results)

	if report == "" {
		t.Fatal("GenerateReport returned empty string")
	}

	// Check that report contains expected sections
	expectedSections := []string{
		"Test Report for test-adapter Adapter",
		"Total Tests: 50",
		"Passed: 48",
		"Failed: 2",
		"Success Rate: 96.00%",
		"Interface Coverage:",
		"Performance Benchmarks:",
		"Errors and Failures:",
		"Recommendations:",
	}

	for _, section := range expectedSections {
		if !contains(report, section) {
			t.Errorf("Report missing expected section: %s", section)
		}
	}
}

// Helper function to check if a string contains a substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > len(substr) && (s[:len(substr)] == substr ||
			s[len(s)-len(substr):] == substr ||
			containsSubstring(s, substr))))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// TestSuiteStructure tests that the TestSuite has the expected structure.
func TestSuiteStructure(t *testing.T) {
	mockEngine := &MockEngine{}
	suite := NewTestSuite("structure-test", mockEngine)

	// Test that all required fields are present and properly typed
	if suite.engine == nil {
		t.Error("Engine field is nil")
	}

	if suite.name == "" {
		t.Error("Name field is empty")
	}

	// Test that config has default values
	if suite.config.RequestTimeout == 0 {
		t.Error("Config RequestTimeout not set to default")
	}

	if suite.config.ConcurrentUsers == 0 {
		t.Error("Config ConcurrentUsers not set to default")
	}
}
