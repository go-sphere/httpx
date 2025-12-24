package testing

import (
	"testing"

	"github.com/go-sphere/httpx"
)

// StateStoreTester tests the StateStore interface methods
type StateStoreTester struct {
	engine httpx.Engine
}

// NewStateStoreTester creates a new StateStore interface tester
func NewStateStoreTester(engine httpx.Engine) *StateStoreTester {
	return &StateStoreTester{engine: engine}
}

// TestSetAndGet tests the Set() and Get() methods together
func (sst *StateStoreTester) TestSetAndGet(t *testing.T) {
	t.Helper()
	
	testCases := []struct {
		name  string
		key   string
		value interface{}
	}{
		{"String value", "string_key", "test_value"},
		{"Integer value", "int_key", 42},
		{"Boolean value", "bool_key", true},
		{"Struct value", "struct_key", TestStruct{Name: "test", Age: 25, Email: "test@example.com"}},
		{"Nil value", "nil_key", nil},
		{"Empty string key", "", "empty_key_value"},
		{"Complex key", "complex.key:with-special_chars", "complex_value"},
	}
	
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			router := sst.engine.Group("")
			// var capturedContext httpx.Context
			
			router.GET(GenerateUniqueTestPath(), func(ctx httpx.Context) {
				// capturedContext = ctx
				
				// Test Set operation
				ctx.Set(tc.key, tc.value)
				
				// Test Get operation immediately after Set
				retrievedValue, exists := ctx.Get(tc.key)
				
				AssertEqual(t, true, exists, "Key should exist after Set")
				AssertEqual(t, tc.value, retrievedValue, "Retrieved value should match set value")
				
				ctx.Text(200, "OK")
			})
			
			t.Logf("Test %s completed", tc.name)
		})
	}
}

// TestNonExistentKey tests the Get() method with non-existent keys
func (sst *StateStoreTester) TestNonExistentKey(t *testing.T) {
	t.Helper()
	
	testCases := []struct {
		name string
		key  string
	}{
		{"Non-existent string key", "nonexistent"},
		{"Empty string key", ""},
		{"Special characters key", "!@#$%^&*()"},
	}
	
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			router := sst.engine.Group("")
			// var capturedContext httpx.Context
			
			router.GET(GenerateUniqueTestPath(), func(ctx httpx.Context) {
				// capturedContext = ctx
				
				// Test Get operation on non-existent key
				retrievedValue, exists := ctx.Get(tc.key)
				
				AssertEqual(t, false, exists, "Non-existent key should not exist")
				AssertEqual(t, nil, retrievedValue, "Non-existent key should return nil value")
				
				ctx.Text(200, "OK")
			})
			
			t.Logf("Test %s completed", tc.name)
		})
	}
}

// TestStateOverwrite tests overwriting existing state values
func (sst *StateStoreTester) TestStateOverwrite(t *testing.T) {
	t.Helper()
	
	testCases := []struct {
		name         string
		key          string
		initialValue interface{}
		newValue     interface{}
	}{
		{"String overwrite", "key1", "initial", "updated"},
		{"Type change", "key2", 42, "string_value"},
		{"Nil overwrite", "key3", "value", nil},
		{"Overwrite nil", "key4", nil, "new_value"},
		{"Same value overwrite", "key5", "same", "same"},
	}
	
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			router := sst.engine.Group("")
			// var capturedContext httpx.Context
			
			router.GET(GenerateUniqueTestPath(), func(ctx httpx.Context) {
				// capturedContext = ctx
				
				// Set initial value
				ctx.Set(tc.key, tc.initialValue)
				
				// Verify initial value
				initialRetrieved, exists := ctx.Get(tc.key)
				AssertEqual(t, true, exists, "Initial value should exist")
				AssertEqual(t, tc.initialValue, initialRetrieved, "Initial value should match")
				
				// Overwrite with new value
				ctx.Set(tc.key, tc.newValue)
				
				// Verify new value
				newRetrieved, exists := ctx.Get(tc.key)
				AssertEqual(t, true, exists, "Overwritten value should exist")
				AssertEqual(t, tc.newValue, newRetrieved, "Overwritten value should match new value")
				
				ctx.Text(200, "OK")
			})
			
			t.Logf("Test %s completed", tc.name)
		})
	}
}

// TestRequestIsolation tests that state is isolated between different requests
func (sst *StateStoreTester) TestRequestIsolation(t *testing.T) {
	t.Helper()
	
	testCases := []struct {
		name string
		key  string
	}{
		{"Isolation test 1", "isolation_key_1"},
		{"Isolation test 2", "isolation_key_2"},
	}
	
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			router := sst.engine.Group("")
			// var capturedContext httpx.Context
			
			// First request - set a value
			router.GET(GenerateUniquePath("set"), func(ctx httpx.Context) {
				// capturedContext = ctx
				ctx.Set(tc.key, "first_request_value")
				
				// Verify value exists in first request
				value, exists := ctx.Get(tc.key)
				AssertEqual(t, true, exists, "Value should exist in first request")
				AssertEqual(t, "first_request_value", value, "Value should match in first request")
				
				ctx.Text(200, "Set OK")
			})
			
			// Second request - should not see the value from first request
			router.GET(GenerateUniquePath("get"), func(ctx httpx.Context) {
				// capturedContext = ctx
				
				// Value from first request should not exist
				value, exists := ctx.Get(tc.key)
				AssertEqual(t, false, exists, "Value from previous request should not exist")
				AssertEqual(t, nil, value, "Value from previous request should be nil")
				
				// Set a different value in second request
				ctx.Set(tc.key, "second_request_value")
				
				// Verify new value exists
				newValue, exists := ctx.Get(tc.key)
				AssertEqual(t, true, exists, "New value should exist in second request")
				AssertEqual(t, "second_request_value", newValue, "New value should match in second request")
				
				ctx.Text(200, "Get OK")
			})
			
			t.Logf("Test %s completed", tc.name)
		})
	}
}

// TestStateInMiddleware tests state sharing between middleware and handlers
func (sst *StateStoreTester) TestStateInMiddleware(t *testing.T) {
	t.Helper()
	
	testCases := []struct {
		name            string
		middlewareKey   string
		middlewareValue interface{}
		handlerKey      string
		handlerValue    interface{}
	}{
		{"String values", "middleware_key", "middleware_value", "handler_key", "handler_value"},
		{"Mixed types", "user_id", 123, "user_name", "john_doe"},
		{"Struct sharing", "config", TestStruct{Name: "config", Age: 1}, "status", "active"},
	}
	
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			router := sst.engine.Group("")
			// var capturedContext httpx.Context
			
			// Add middleware that sets state
			router.Use(func(ctx httpx.Context) {
				// Set value in middleware
				ctx.Set(tc.middlewareKey, tc.middlewareValue)
				
				// Verify middleware can read its own value
				middlewareRetrieved, exists := ctx.Get(tc.middlewareKey)
				AssertEqual(t, true, exists, "Middleware should be able to read its own value")
				AssertEqual(t, tc.middlewareValue, middlewareRetrieved, "Middleware value should match")
			})
			
			router.GET(GenerateUniqueTestPath(), func(ctx httpx.Context) {
				// capturedContext = ctx
				
				// Handler should be able to read middleware value
				middlewareRetrieved, exists := ctx.Get(tc.middlewareKey)
				AssertEqual(t, true, exists, "Handler should be able to read middleware value")
				AssertEqual(t, tc.middlewareValue, middlewareRetrieved, "Handler should get correct middleware value")
				
				// Handler sets its own value
				ctx.Set(tc.handlerKey, tc.handlerValue)
				
				// Handler should be able to read its own value
				handlerRetrieved, exists := ctx.Get(tc.handlerKey)
				AssertEqual(t, true, exists, "Handler should be able to read its own value")
				AssertEqual(t, tc.handlerValue, handlerRetrieved, "Handler value should match")
				
				// Handler should still be able to read middleware value
				middlewareStillThere, exists := ctx.Get(tc.middlewareKey)
				AssertEqual(t, true, exists, "Middleware value should still exist after handler sets value")
				AssertEqual(t, tc.middlewareValue, middlewareStillThere, "Middleware value should remain unchanged")
				
				ctx.Text(200, "OK")
			})
			
			t.Logf("Test %s completed", tc.name)
		})
	}
}

// RunAllTests runs all StateStore interface tests
func (sst *StateStoreTester) RunAllTests(t *testing.T) {
	t.Helper()
	t.Run("SetAndGet", sst.TestSetAndGet)
	t.Run("NonExistentKey", sst.TestNonExistentKey)
	t.Run("StateOverwrite", sst.TestStateOverwrite)
	t.Run("RequestIsolation", sst.TestRequestIsolation)
	t.Run("StateInMiddleware", sst.TestStateInMiddleware)
}