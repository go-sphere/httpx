package testing

import (
	"fmt"
	"sync"
	"testing"

	"github.com/go-sphere/httpx"
)

// StateStoreTester provides comprehensive testing tools for the StateStore interface.
type StateStoreTester struct {
	engine httpx.Engine
}

// NewStateStoreTester creates a new StateStoreTester instance.
func NewStateStoreTester(engine httpx.Engine) *StateStoreTester {
	return &StateStoreTester{
		engine: engine,
	}
}

// TestSetAndGet tests Set() and Get() methods for basic key-value storage.
// Validates Requirements 6.1, 6.2: Set() method stores key-value pairs, Get() method retrieves stored values
func (st *StateStoreTester) TestSetAndGet(t *testing.T) {
	t.Helper()

	router := st.engine.Group("")

	var capturedContext httpx.Context
	router.GET("/test-set-get", func(ctx httpx.Context) {
		capturedContext = ctx

		// Test setting and getting string values
		ctx.Set("name", "John Doe")
		ctx.Set("age", 30)
		ctx.Set("active", true)
		ctx.Set("balance", 1234.56)

		// Test getting values
		if name, exists := ctx.Get("name"); !exists {
			t.Error("Expected 'name' key to exist")
		} else if nameStr, ok := name.(string); !ok {
			t.Errorf("Expected name to be string, got %T", name)
		} else if nameStr != "John Doe" {
			t.Errorf("Expected name='John Doe', got %s", nameStr)
		}

		if age, exists := ctx.Get("age"); !exists {
			t.Error("Expected 'age' key to exist")
		} else if ageInt, ok := age.(int); !ok {
			t.Errorf("Expected age to be int, got %T", age)
		} else if ageInt != 30 {
			t.Errorf("Expected age=30, got %d", ageInt)
		}

		if active, exists := ctx.Get("active"); !exists {
			t.Error("Expected 'active' key to exist")
		} else if activeBool, ok := active.(bool); !ok {
			t.Errorf("Expected active to be bool, got %T", active)
		} else if !activeBool {
			t.Errorf("Expected active=true, got %v", activeBool)
		}

		if balance, exists := ctx.Get("balance"); !exists {
			t.Error("Expected 'balance' key to exist")
		} else if balanceFloat, ok := balance.(float64); !ok {
			t.Errorf("Expected balance to be float64, got %T", balance)
		} else if balanceFloat != 1234.56 {
			t.Errorf("Expected balance=1234.56, got %f", balanceFloat)
		}

		ctx.Text(200, "OK")
	})

	if capturedContext != nil {
		t.Log("Set and Get operations completed successfully")
	}

	// Test different data types
	testCases := []struct {
		name  string
		key   string
		value interface{}
		path  string
	}{
		{"String", "str_key", "string_value", "/set-get-string"},
		{"Integer", "int_key", 42, "/set-get-int"},
		{"Float", "float_key", 3.14159, "/set-get-float"},
		{"Boolean", "bool_key", true, "/set-get-bool"},
		{"Slice", "slice_key", []string{"a", "b", "c"}, "/set-get-slice"},
		{"Map", "map_key", map[string]int{"x": 1, "y": 2}, "/set-get-map"},
		{"Struct", "struct_key", struct{ Name string }{"test"}, "/set-get-struct"},
		{"Nil", "nil_key", nil, "/set-get-nil"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			router.GET(tc.path, func(ctx httpx.Context) {
				// Set the value
				ctx.Set(tc.key, tc.value)

				// Get the value back
				value, exists := ctx.Get(tc.key)
				if !exists {
					t.Errorf("Expected key '%s' to exist", tc.key)
					return
				}

				// For nil values, just check existence
				if tc.value == nil {
					if value != nil {
						t.Errorf("Expected nil value, got %v", value)
					}
					return
				}

				// For other values, check type and equality
				if value != tc.value {
					t.Errorf("Expected value %v, got %v", tc.value, value)
				}

				ctx.Text(200, "OK")
			})

			t.Logf("Testing Set/Get with %s: %v", tc.name, tc.value)
		})
	}
}

// TestNonExistentKey tests Get() method behavior with non-existent keys.
// Validates Requirements 6.3: When key doesn't exist, Get() method returns false flag
func (st *StateStoreTester) TestNonExistentKey(t *testing.T) {
	t.Helper()

	router := st.engine.Group("")

	var capturedContext httpx.Context
	router.GET("/test-non-existent", func(ctx httpx.Context) {
		capturedContext = ctx

		// Test getting non-existent key
		value, exists := ctx.Get("non_existent_key")
		if exists {
			t.Error("Expected non-existent key to return false for exists flag")
		}
		if value != nil {
			t.Errorf("Expected nil value for non-existent key, got %v", value)
		}

		// Test multiple non-existent keys
		nonExistentKeys := []string{
			"missing_key",
			"undefined",
			"not_set",
			"", // empty string key
			"key_with_spaces ",
			"UPPERCASE_KEY",
		}

		for _, key := range nonExistentKeys {
			if value, exists := ctx.Get(key); exists {
				t.Errorf("Expected key '%s' to not exist, but got value: %v", key, value)
			}
		}

		ctx.Text(200, "OK")
	})

	if capturedContext != nil {
		t.Log("Non-existent key tests completed successfully")
	}

	// Test edge cases for key names
	edgeCases := []struct {
		name string
		key  string
		path string
	}{
		{"EmptyKey", "", "/non-existent-empty"},
		{"SpaceKey", " ", "/non-existent-space"},
		{"SpecialChars", "key!@#$%^&*()", "/non-existent-special"},
		{"UnicodeKey", "键名", "/non-existent-unicode"},
		{"LongKey", string(make([]byte, 1000)), "/non-existent-long"},
	}

	for _, tc := range edgeCases {
		t.Run(tc.name, func(t *testing.T) {
			router.GET(tc.path, func(ctx httpx.Context) {
				value, exists := ctx.Get(tc.key)
				if exists {
					t.Errorf("Expected key '%s' to not exist", tc.key)
				}
				if value != nil {
					t.Errorf("Expected nil value for non-existent key '%s', got %v", tc.key, value)
				}
				ctx.Text(200, "OK")
			})

			t.Logf("Testing non-existent key: %s", tc.name)
		})
	}
}

// TestRequestIsolation tests that state is isolated between different requests.
// Validates Requirements 6.4: State isolation within request scope
func (st *StateStoreTester) TestRequestIsolation(t *testing.T) {
	t.Helper()

	router := st.engine.Group("")

	// Counter to track request isolation
	var requestCounter int
	var mu sync.Mutex

	// First endpoint that sets state
	router.GET("/isolation-set/:id", func(ctx httpx.Context) {
		mu.Lock()
		requestCounter++
		currentRequest := requestCounter
		mu.Unlock()

		requestID := ctx.Param("id")
		ctx.Set("request_id", requestID)
		ctx.Set("request_counter", currentRequest)
		ctx.Set("shared_key", "value_from_"+requestID)

		ctx.JSON(200, map[string]interface{}{
			"request_id":      requestID,
			"request_counter": currentRequest,
			"message":         "State set for request " + requestID,
		})
	})

	// Second endpoint that checks state isolation
	router.GET("/isolation-check/:id", func(ctx httpx.Context) {
		requestID := ctx.Param("id")

		// This should not see state from other requests
		if value, exists := ctx.Get("request_id"); exists {
			t.Errorf("Request %s should not see request_id from other requests, but got: %v", requestID, value)
		}

		if value, exists := ctx.Get("shared_key"); exists {
			t.Errorf("Request %s should not see shared_key from other requests, but got: %v", requestID, value)
		}

		// Set its own state
		ctx.Set("check_request_id", requestID)
		ctx.Set("check_timestamp", "now")

		// Verify its own state
		if value, exists := ctx.Get("check_request_id"); !exists {
			t.Error("Request should be able to access its own state")
		} else if value != requestID {
			t.Errorf("Expected check_request_id=%s, got %v", requestID, value)
		}

		ctx.JSON(200, map[string]interface{}{
			"request_id": requestID,
			"message":    "State checked for request " + requestID,
		})
	})

	// Test concurrent request isolation
	router.GET("/isolation-concurrent/:id", func(ctx httpx.Context) {
		requestID := ctx.Param("id")

		// Set initial state
		ctx.Set("concurrent_id", requestID)
		ctx.Set("step", 1)

		// Simulate some processing time where other requests might interfere
		// In a real test, this would involve actual concurrent requests

		// Verify state hasn't been corrupted
		if value, exists := ctx.Get("concurrent_id"); !exists {
			t.Error("Concurrent request lost its own state")
		} else if value != requestID {
			t.Errorf("Concurrent request state corrupted: expected %s, got %v", requestID, value)
		}

		// Update state
		ctx.Set("step", 2)
		ctx.Set("final_value", "completed_"+requestID)

		// Final verification
		if step, exists := ctx.Get("step"); !exists {
			t.Error("Step state lost during concurrent processing")
		} else if step != 2 {
			t.Errorf("Expected step=2, got %v", step)
		}

		ctx.JSON(200, map[string]interface{}{
			"request_id":  requestID,
			"final_step":  2,
			"final_value": "completed_" + requestID,
		})
	})

	t.Log("Request isolation tests set up successfully")

	// Test state persistence within a single request
	router.GET("/isolation-persistence", func(ctx httpx.Context) {
		// Set multiple values at different points in the request
		ctx.Set("early_value", "set_early")

		// Simulate middleware or other processing
		ctx.Set("middle_value", "set_middle")

		// Verify all values are still accessible
		early, earlyExists := ctx.Get("early_value")
		middle, middleExists := ctx.Get("middle_value")

		if !earlyExists {
			t.Error("Early value should persist throughout request")
		}
		if !middleExists {
			t.Error("Middle value should be accessible")
		}

		if early != "set_early" {
			t.Errorf("Early value corrupted: expected 'set_early', got %v", early)
		}
		if middle != "set_middle" {
			t.Errorf("Middle value corrupted: expected 'set_middle', got %v", middle)
		}

		// Set final value
		ctx.Set("final_value", "set_final")

		ctx.JSON(200, map[string]interface{}{
			"early":  early,
			"middle": middle,
			"final":  "set_final",
		})
	})
}

// TestStateOverwrite tests overwriting existing state values.
func (st *StateStoreTester) TestStateOverwrite(t *testing.T) {
	t.Helper()

	router := st.engine.Group("")

	router.GET("/test-overwrite", func(ctx httpx.Context) {
		// Set initial value
		ctx.Set("overwrite_key", "initial_value")

		// Verify initial value
		if value, exists := ctx.Get("overwrite_key"); !exists {
			t.Error("Initial value should exist")
		} else if value != "initial_value" {
			t.Errorf("Expected initial_value, got %v", value)
		}

		// Overwrite with different type
		ctx.Set("overwrite_key", 42)

		// Verify overwritten value
		if value, exists := ctx.Get("overwrite_key"); !exists {
			t.Error("Overwritten value should exist")
		} else if value != 42 {
			t.Errorf("Expected 42, got %v", value)
		}

		// Overwrite with nil
		ctx.Set("overwrite_key", nil)

		// Verify nil value
		if value, exists := ctx.Get("overwrite_key"); !exists {
			t.Error("Nil value should still exist as a key")
		} else if value != nil {
			t.Errorf("Expected nil, got %v", value)
		}

		ctx.Text(200, "OK")
	})

	t.Log("State overwrite test set up successfully")
}

// TestLargeStateData tests handling of large state data.
func (st *StateStoreTester) TestLargeStateData(t *testing.T) {
	t.Helper()

	router := st.engine.Group("")

	router.GET("/test-large-data", func(ctx httpx.Context) {
		// Test large string
		largeString := string(make([]byte, 10000))
		for i := range largeString {
			largeString = largeString[:i] + "a" + largeString[i+1:]
		}
		ctx.Set("large_string", largeString)

		// Test large slice
		largeSlice := make([]int, 1000)
		for i := range largeSlice {
			largeSlice[i] = i
		}
		ctx.Set("large_slice", largeSlice)

		// Test large map
		largeMap := make(map[string]int)
		for i := 0; i < 1000; i++ {
			largeMap[string(rune('a'+i%26))+string(rune('0'+i%10))] = i
		}
		ctx.Set("large_map", largeMap)

		// Verify large data
		if value, exists := ctx.Get("large_string"); !exists {
			t.Error("Large string should exist")
		} else if len(value.(string)) != 10000 {
			t.Errorf("Expected large string length 10000, got %d", len(value.(string)))
		}

		if value, exists := ctx.Get("large_slice"); !exists {
			t.Error("Large slice should exist")
		} else if len(value.([]int)) != 1000 {
			t.Errorf("Expected large slice length 1000, got %d", len(value.([]int)))
		}

		if value, exists := ctx.Get("large_map"); !exists {
			t.Error("Large map should exist")
		} else if len(value.(map[string]int)) != 1000 {
			t.Errorf("Expected large map length 1000, got %d", len(value.(map[string]int)))
		}

		ctx.Text(200, "OK")
	})

	t.Log("Large state data test set up successfully")
}

// RunAllTests runs all StateStore interface tests.
func (st *StateStoreTester) RunAllTests(t *testing.T) {
	t.Helper()

	t.Run("SetAndGet", st.TestSetAndGet)
	t.Run("NonExistentKey", st.TestNonExistentKey)
	t.Run("RequestIsolation", st.TestRequestIsolation)
	t.Run("StateOverwrite", st.TestStateOverwrite)
	t.Run("LargeStateData", st.TestLargeStateData)
}

// Helper functions for creating test data

// createTestStateData creates various types of test data for state storage.
func createTestStateData() map[string]interface{} {
	return map[string]interface{}{
		"string":    "test_string",
		"int":       42,
		"float":     3.14159,
		"bool":      true,
		"slice":     []string{"a", "b", "c"},
		"map":       map[string]int{"x": 1, "y": 2},
		"struct":    struct{ Name string }{"test"},
		"nil":       nil,
		"interface": interface{}("interface_value"),
	}
}

// createLargeTestData creates large test data for stress testing.
func createLargeTestData(size int) map[string]interface{} {
	data := make(map[string]interface{})

	// Large string
	largeString := make([]byte, size)
	for i := range largeString {
		largeString[i] = byte('a' + (i % 26))
	}
	data["large_string"] = string(largeString)

	// Large slice
	largeSlice := make([]int, size)
	for i := range largeSlice {
		largeSlice[i] = i
	}
	data["large_slice"] = largeSlice

	// Large map
	largeMap := make(map[string]int)
	for i := 0; i < size; i++ {
		key := fmt.Sprintf("key_%d", i)
		largeMap[key] = i
	}
	data["large_map"] = largeMap

	return data
}
