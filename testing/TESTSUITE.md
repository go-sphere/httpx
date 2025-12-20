# httpx Testing Framework - TestSuite

The TestSuite provides a comprehensive testing solution for httpx adapter implementations. It integrates all testing tools into a unified interface for validating adapter correctness, performance, and thread safety.

## Overview

The TestSuite combines the following testing tools:
- **AbortTracker**: Tests middleware execution and abort functionality
- **RequestTester**: Tests Request interface implementation
- **BinderTester**: Tests data binding functionality
- **ResponderTester**: Tests response writing functionality
- **StateStoreTester**: Tests request-scoped state management
- **RouterTester**: Tests routing and middleware functionality
- **EngineTester**: Tests server lifecycle management

## Usage

### Basic Usage

```go
package main

import (
    "testing"
    "github.com/go-sphere/httpx/testing"
)

func TestMyAdapter(t *testing.T) {
    // Create your adapter engine
    engine := myAdapter.NewEngine()
    
    // Create the test suite
    suite := testing.NewTestSuite("my-adapter", engine)
    
    // Run comprehensive tests
    suite.RunAllTests(t)
}
```

### Advanced Usage with Custom Configuration

```go
func TestMyAdapterWithConfig(t *testing.T) {
    engine := myAdapter.NewEngine()
    
    // Custom test configuration
    config := testing.TestConfig{
        ServerAddr:      ":9090",
        RequestTimeout:  10 * time.Second,
        ConcurrentUsers: 50,
        TestDataSize:    2048,
    }
    
    suite := testing.NewTestSuiteWithConfig("my-adapter", engine, config)
    
    // Run all test types
    suite.RunAllTests(t)
    suite.RunConcurrencyTests(t)
}
```

### Performance Benchmarking

```go
func BenchmarkMyAdapter(b *testing.B) {
    engine := myAdapter.NewEngine()
    suite := testing.NewTestSuite("my-adapter", engine)
    
    // Run performance benchmarks
    suite.RunBenchmarks(b)
}
```

### Generating Test Reports

```go
func TestWithReport(t *testing.T) {
    engine := myAdapter.NewEngine()
    suite := testing.NewTestSuite("my-adapter", engine)
    
    // Run tests and collect results
    suite.RunAllTests(t)
    
    // Generate comprehensive report
    results := &testing.TestResults{
        TotalTests:   100,
        PassedTests:  98,
        FailedTests:  2,
        SkippedTests: 0,
        Duration:     5 * time.Second,
        InterfaceCoverage: map[string]float64{
            "Request":    100.0,
            "Responder":  98.0,
            "StateStore": 100.0,
            "Router":     95.0,
            "Engine":     100.0,
        },
        BenchmarkResults: map[string]string{
            "BasicRequest": "1000 ns/op",
            "JSONResponse": "2000 ns/op",
        },
        Errors: []string{
            "Test failed: expected X, got Y",
        },
    }
    
    report := suite.GenerateReport(results)
    t.Log(report)
}
```

## Test Categories

### 1. Interface Tests (`RunAllTests`)

Tests all httpx interfaces for correctness:
- Request data access (params, queries, headers, cookies, body)
- Data binding (JSON, query, form, URI, header)
- Response writing (JSON, text, files, redirects, headers, cookies)
- State management (set/get with request isolation)
- Routing (registration, groups, middleware, static files)
- Engine lifecycle (start/stop, running status, address)
- Abort functionality (middleware chain interruption)

### 2. Concurrency Tests (`RunConcurrencyTests`)

Tests thread safety and concurrent behavior:
- Multiple simultaneous requests
- Concurrent state store access
- Concurrent middleware execution
- Concurrent router operations

### 3. Performance Benchmarks (`RunBenchmarks`)

Measures performance characteristics:
- Basic request handling
- JSON response generation
- State store operations
- Middleware execution overhead
- Parameter parsing performance

## Configuration Options

The `TestConfig` struct allows customization:

```go
type TestConfig struct {
    ServerAddr      string        // Server listening address
    RequestTimeout  time.Duration // HTTP request timeout
    ConcurrentUsers int           // Number of concurrent test users
    TestDataSize    int           // Size of test data in bytes
}
```

Default configuration:
```go
var DefaultTestConfig = TestConfig{
    ServerAddr:      ":0",                // Random port
    RequestTimeout:  5 * time.Second,
    ConcurrentUsers: 10,
    TestDataSize:    1024,
}
```

## Test Results and Reporting

The TestSuite generates comprehensive reports including:
- Test execution summary (passed/failed/skipped)
- Success rate percentage
- Interface coverage metrics
- Performance benchmark results
- Error details and recommendations

## Requirements Validation

The TestSuite validates the following requirements:
- **13.1**: Complete test suite covering all interfaces
- **13.2**: Concurrency testing for thread safety
- **13.3**: Performance benchmarking tools
- **13.4**: Detailed test reporting
- **13.5**: Clear error messages for implementation issues

## Integration with Adapters

Each httpx adapter should include tests using the TestSuite:

```go
// In ginx/engine_test.go
func TestGinxAdapter(t *testing.T) {
    engine := ginx.New()
    suite := testing.NewTestSuite("ginx", engine)
    suite.RunAllTests(t)
}

// In fiberx/engine_test.go  
func TestFiberxAdapter(t *testing.T) {
    engine := fiberx.New()
    suite := testing.NewTestSuite("fiberx", engine)
    suite.RunAllTests(t)
}
```

This ensures consistent behavior across all httpx adapters and validates that each adapter correctly implements the httpx protocol interfaces.