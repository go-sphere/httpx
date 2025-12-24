package integration

import (
	"context"
	"io/fs"
	"testing"

	"github.com/go-sphere/httpx"
	httptesting "github.com/go-sphere/httpx/testing"
)

// MockEngine is a simple mock engine for testing the common integration functionality
type MockEngine struct{}

func (m *MockEngine) Group(prefix string, middleware ...httpx.Middleware) httpx.Router { return &MockRouter{} }
func (m *MockEngine) Start() error                                                     { return nil }
func (m *MockEngine) Stop(ctx context.Context) error                                   { return nil }
func (m *MockEngine) IsRunning() bool                                                  { return false }
func (m *MockEngine) Use(middleware ...httpx.Middleware)                               {}

type MockRouter struct{}

func (m *MockRouter) GET(path string, handler httpx.Handler)                         {}
func (m *MockRouter) POST(path string, handler httpx.Handler)                        {}
func (m *MockRouter) PUT(path string, handler httpx.Handler)                         {}
func (m *MockRouter) DELETE(path string, handler httpx.Handler)                      {}
func (m *MockRouter) PATCH(path string, handler httpx.Handler)                       {}
func (m *MockRouter) HEAD(path string, handler httpx.Handler)                        {}
func (m *MockRouter) OPTIONS(path string, handler httpx.Handler)                     {}
func (m *MockRouter) Any(path string, handler httpx.Handler)                         {}
func (m *MockRouter) Handle(method, path string, handler httpx.Handler)              {}
func (m *MockRouter) Group(prefix string, middleware ...httpx.Middleware) httpx.Router { return &MockRouter{} }
func (m *MockRouter) Use(middleware ...httpx.Middleware)                             {}
func (m *MockRouter) BasePath() string                                              { return "" }
func (m *MockRouter) Static(prefix, root string)                                    {}
func (m *MockRouter) StaticFS(prefix string, filesystem fs.FS)                      {}

// TestCommonIntegrationTestsCreation tests that CommonIntegrationTests can be created properly
func TestCommonIntegrationTestsCreation(t *testing.T) {
	engine := &MockEngine{}
	
	// Test creation without config
	cit := NewCommonIntegrationTests("mock", engine)
	if cit == nil {
		t.Fatal("CommonIntegrationTests should not be nil")
	}
	
	if cit.GetFrameworkName() != "mock" {
		t.Errorf("Expected framework name 'mock', got '%s'", cit.GetFrameworkName())
	}
	
	if cit.GetEngine() != engine {
		t.Error("Engine should match the provided engine")
	}
	
	if cit.GetTestSuite() == nil {
		t.Error("Test suite should not be nil")
	}
	
	if cit.GetConfig() == nil {
		t.Error("Config should not be nil (should use default)")
	}
}

// TestCommonIntegrationTestsWithConfig tests creation with custom config
func TestCommonIntegrationTestsWithConfig(t *testing.T) {
	engine := &MockEngine{}
	config := &httptesting.TestConfig{
		ServerAddr:     ":8080",
		VerboseLogging: true,
	}
	
	cit := NewCommonIntegrationTestsWithConfig("mock", engine, config)
	if cit == nil {
		t.Fatal("CommonIntegrationTests should not be nil")
	}
	
	if cit.GetConfig().ServerAddr != ":8080" {
		t.Errorf("Expected server addr ':8080', got '%s'", cit.GetConfig().ServerAddr)
	}
	
	if !cit.GetConfig().VerboseLogging {
		t.Error("VerboseLogging should be true")
	}
}

// TestTestSkipManager tests the test skip manager functionality
func TestTestSkipManager(t *testing.T) {
	skipManager := NewTestSkipManager()
	
	// Add a skipped test
	skipManager.AddSkippedTest("fiberx", "Binder", "BindJSON", "Known issue with JSON binding")
	
	// Test that the test should be skipped
	shouldSkip, reason := skipManager.ShouldSkipTest("fiberx", "Binder", "BindJSON")
	if !shouldSkip {
		t.Error("Test should be skipped")
	}
	
	if reason != "Known issue with JSON binding" {
		t.Errorf("Expected reason 'Known issue with JSON binding', got '%s'", reason)
	}
	
	// Test that a non-skipped test should not be skipped
	shouldSkip, _ = skipManager.ShouldSkipTest("ginx", "Binder", "BindJSON")
	if shouldSkip {
		t.Error("Test should not be skipped for ginx")
	}
	
	// Test getting skipped tests
	skippedTests := skipManager.GetSkippedTests("fiberx")
	if len(skippedTests) != 1 {
		t.Errorf("Expected 1 skipped test, got %d", len(skippedTests))
	}
}

// TestValidateFrameworkIntegration tests the framework integration validation
func TestValidateFrameworkIntegration(t *testing.T) {
	engine := &MockEngine{}
	cit := NewCommonIntegrationTests("mock", engine)
	
	// This should not fail with our mock engine
	// Note: This test will likely fail because MockEngine doesn't fully implement
	// all the required interfaces, but it demonstrates the validation functionality
	t.Run("ValidateIntegration", func(t *testing.T) {
		// We expect this to potentially fail with mock engine since it's not a real implementation
		// but it should not panic
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("ValidateFrameworkIntegration should not panic: %v", r)
			}
		}()
		
		cit.ValidateFrameworkIntegration(t)
	})
}