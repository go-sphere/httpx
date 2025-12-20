package testing

import (
	"fmt"
	"time"
)

// TestError represents a testing framework error with detailed context.
type TestError struct {
	Component string
	Operation string
	Expected  interface{}
	Actual    interface{}
	Message   string
}

// Error implements the error interface for TestError.
func (e *TestError) Error() string {
	return fmt.Sprintf("%s.%s: expected %v, got %v - %s",
		e.Component, e.Operation, e.Expected, e.Actual, e.Message)
}

// NewTestError creates a new TestError with the provided details.
func NewTestError(component, operation string, expected, actual interface{}, message string) *TestError {
	return &TestError{
		Component: component,
		Operation: operation,
		Expected:  expected,
		Actual:    actual,
		Message:   message,
	}
}

// TestConfig holds configuration for the testing framework.
type TestConfig struct {
	ServerAddr      string
	RequestTimeout  time.Duration
	ConcurrentUsers int
	TestDataSize    int
}

// DefaultTestConfig provides sensible defaults for testing configuration.
var DefaultTestConfig = TestConfig{
	ServerAddr:      ":0", // Random port
	RequestTimeout:  5 * time.Second,
	ConcurrentUsers: 10,
	TestDataSize:    1024,
}

// TestStruct is a standard structure used for binding tests across all adapters.
type TestStruct struct {
	Name  string `json:"name" form:"name" query:"name" uri:"name" header:"X-Name"`
	Age   int    `json:"age" form:"age" query:"age" uri:"age" header:"X-Age"`
	Email string `json:"email" form:"email" query:"email"`
}

// NestedTestStruct is used for complex binding tests with nested structures.
type NestedTestStruct struct {
	User   TestStruct `json:"user" form:"user"`
	Active bool       `json:"active" form:"active"`
	Tags   []string   `json:"tags" form:"tags"`
}
