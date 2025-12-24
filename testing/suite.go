package testing

import (
	"testing"

	"github.com/go-sphere/httpx"
)

// TestSuite coordinates all interface testers for comprehensive testing
type TestSuite struct {
	name   string
	engine httpx.Engine
	config *TestConfig
	helper *TestHelper
	
	// Interface testers
	requestInfoTester *RequestInfoTester
	requestTester     *RequestTester
	bodyAccessTester  *BodyAccessTester
	formAccessTester  *FormAccessTester
	binderTester      *BinderTester
	responderTester   *ResponderTester
	stateStoreTester  *StateStoreTester
	aborterTester     *AborterTester
	routerTester      *RouterTester
	engineTester      *EngineTester
}

// NewTestSuite creates a new test suite for the given engine
func NewTestSuite(name string, engine httpx.Engine) *TestSuite {
	return NewTestSuiteWithConfig(name, engine, nil)
}

// NewTestSuiteWithConfig creates a new test suite with custom configuration
func NewTestSuiteWithConfig(name string, engine httpx.Engine, config *TestConfig) *TestSuite {
	if config == nil {
		config = DefaultTestConfig()
	}
	
	helper := NewTestHelper(config)
	
	return &TestSuite{
		name:   name,
		engine: engine,
		config: config,
		helper: helper,
		
		// Initialize all interface testers
		requestInfoTester: NewRequestInfoTester(engine),
		requestTester:     NewRequestTester(engine),
		bodyAccessTester:  NewBodyAccessTester(engine),
		formAccessTester:  NewFormAccessTester(engine),
		binderTester:      NewBinderTester(engine),
		responderTester:   NewResponderTester(engine),
		stateStoreTester:  NewStateStoreTester(engine),
		aborterTester:     NewAborterTester(engine),
		routerTester:      NewRouterTester(engine),
		engineTester:      NewEngineTester(engine),
	}
}

// RunAllTests runs all interface tests in the suite
func (ts *TestSuite) RunAllTests(t *testing.T) {
	t.Helper()
	
	t.Logf("Running test suite for: %s", ts.name)
	
	// Test individual interfaces
	t.Run("RequestInfo", ts.requestInfoTester.RunAllTests)
	t.Run("Request", ts.requestTester.RunAllTests)
	t.Run("BodyAccess", ts.bodyAccessTester.RunAllTests)
	t.Run("FormAccess", ts.formAccessTester.RunAllTests)
	t.Run("Binder", ts.binderTester.RunAllTests)
	t.Run("Responder", ts.responderTester.RunAllTests)
	t.Run("StateStore", ts.stateStoreTester.RunAllTests)
	t.Run("Aborter", ts.aborterTester.RunAllTests)
	t.Run("Router", ts.routerTester.RunAllTests)
	t.Run("Engine", ts.engineTester.RunAllTests)
}

// RunRequestInfoTests runs only RequestInfo interface tests
func (ts *TestSuite) RunRequestInfoTests(t *testing.T) {
	t.Helper()
	ts.requestInfoTester.RunAllTests(t)
}

// RunRequestTests runs only Request composite interface tests
func (ts *TestSuite) RunRequestTests(t *testing.T) {
	t.Helper()
	ts.requestTester.RunAllTests(t)
}

// RunBodyAccessTests runs only BodyAccess interface tests
func (ts *TestSuite) RunBodyAccessTests(t *testing.T) {
	t.Helper()
	ts.bodyAccessTester.RunAllTests(t)
}

// RunFormAccessTests runs only FormAccess interface tests
func (ts *TestSuite) RunFormAccessTests(t *testing.T) {
	t.Helper()
	ts.formAccessTester.RunAllTests(t)
}

// RunBinderTests runs only Binder interface tests
func (ts *TestSuite) RunBinderTests(t *testing.T) {
	t.Helper()
	ts.binderTester.RunAllTests(t)
}

// RunResponderTests runs only Responder interface tests
func (ts *TestSuite) RunResponderTests(t *testing.T) {
	t.Helper()
	ts.responderTester.RunAllTests(t)
}

// RunStateStoreTests runs only StateStore interface tests
func (ts *TestSuite) RunStateStoreTests(t *testing.T) {
	t.Helper()
	ts.stateStoreTester.RunAllTests(t)
}

// RunAborterTests runs only Aborter interface tests
func (ts *TestSuite) RunAborterTests(t *testing.T) {
	t.Helper()
	ts.aborterTester.RunAllTests(t)
}

// RunRouterTests runs only Router interface tests
func (ts *TestSuite) RunRouterTests(t *testing.T) {
	t.Helper()
	ts.routerTester.RunAllTests(t)
}

// RunEngineTests runs only Engine interface tests
func (ts *TestSuite) RunEngineTests(t *testing.T) {
	t.Helper()
	ts.engineTester.RunAllTests(t)
}
// GetRequestInfoTester returns the RequestInfo tester
func (ts *TestSuite) GetRequestInfoTester() *RequestInfoTester {
	return ts.requestInfoTester
}

// GetRequestTester returns the Request composite interface tester
func (ts *TestSuite) GetRequestTester() *RequestTester {
	return ts.requestTester
}

// GetBodyAccessTester returns the BodyAccess tester
func (ts *TestSuite) GetBodyAccessTester() *BodyAccessTester {
	return ts.bodyAccessTester
}

// GetFormAccessTester returns the FormAccess tester
func (ts *TestSuite) GetFormAccessTester() *FormAccessTester {
	return ts.formAccessTester
}

// GetBinderTester returns the Binder tester
func (ts *TestSuite) GetBinderTester() *BinderTester {
	return ts.binderTester
}

// GetResponderTester returns the Responder tester
func (ts *TestSuite) GetResponderTester() *ResponderTester {
	return ts.responderTester
}

// GetStateStoreTester returns the StateStore tester
func (ts *TestSuite) GetStateStoreTester() *StateStoreTester {
	return ts.stateStoreTester
}

// GetAborterTester returns the Aborter tester
func (ts *TestSuite) GetAborterTester() *AborterTester {
	return ts.aborterTester
}

// GetRouterTester returns the Router tester
func (ts *TestSuite) GetRouterTester() *RouterTester {
	return ts.routerTester
}

// GetEngineTester returns the Engine tester
func (ts *TestSuite) GetEngineTester() *EngineTester {
	return ts.engineTester
}

// Name returns the test suite name
func (ts *TestSuite) Name() string {
	return ts.name
}

// Engine returns the engine being tested
func (ts *TestSuite) Engine() httpx.Engine {
	return ts.engine
}

// Config returns the test configuration
func (ts *TestSuite) Config() *TestConfig {
	return ts.config
}

// Helper returns the test helper
func (ts *TestSuite) Helper() *TestHelper {
	return ts.helper
}