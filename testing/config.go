package testing

import (
	"time"
)

// TestConfig holds configuration for testing framework behavior
type TestConfig struct {
	ServerAddr      string        // Server address (default: ":0")
	RequestTimeout  time.Duration // Request timeout (default: 5s)
	ConcurrentUsers int           // Concurrent users for load tests
	TestDataSize    int           // Size of test data payloads
	SkipSlowTests   bool          // Skip tests that are known to be slow
	VerboseLogging  bool          // Enable verbose test logging
	MaxRetries      int           // Maximum number of retries for flaky tests
}

// DefaultTestConfig returns a TestConfig with sensible defaults
func DefaultTestConfig() *TestConfig {
	return &TestConfig{
		ServerAddr:      ":0",
		RequestTimeout:  5 * time.Second,
		ConcurrentUsers: 10,
		TestDataSize:    1024,
		SkipSlowTests:   false,
		VerboseLogging:  false,
		MaxRetries:      3,
	}
}

// TestStruct is a standard test structure for binding tests
type TestStruct struct {
	Name  string `json:"name" form:"name" query:"name" uri:"name" header:"X-Name"`
	Age   int    `json:"age" form:"age" query:"age" uri:"age" header:"X-Age"`
	Email string `json:"email" form:"email" query:"email"`
}

// NestedTestStruct is a nested structure for complex binding tests
type NestedTestStruct struct {
	User    TestStruct `json:"user" form:"user"`
	Address Address    `json:"address" form:"address"`
}

// Address represents an address for nested testing
type Address struct {
	Street string `json:"street" form:"street" query:"street"`
	City   string `json:"city" form:"city" query:"city"`
	Zip    string `json:"zip" form:"zip" query:"zip"`
}
