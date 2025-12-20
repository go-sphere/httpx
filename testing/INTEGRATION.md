# httpx Testing Framework Integration Guide

This guide demonstrates how to integrate the httpx testing framework with different Web framework adapters. The testing framework provides a unified way to test all httpx interface implementations across different adapters.

## Quick Start

### Basic Integration

The simplest way to integrate the testing framework with an adapter:

```go
package myadapter

import (
    "testing"
    "github.com/go-sphere/httpx/testing"
)

func TestMyAdapterIntegration(t *testing.T) {
    // Create your adapter engine
    engine := New(WithServerAddr(":0"))
    
    // Create test suite
    suite := testing.NewTestSuite("myadapter", engine)
    
    // Run all tests
    suite.RunAllTests(t)
}
```

### Advanced Integration

For more control over testing configuration:

```go
func TestMyAdapterAdvanced(t *testing.T) {
    // Custom test configuration
    config := testing.TestConfig{
        ServerAddr:      ":0",
        RequestTimeout:  10 * time.Second,
        ConcurrentUsers: 20,
        TestDataSize:    2048,
    }
    
    // Create engine with custom options
    engine := New(WithCustomOptions(...))
    
    // Create test suite with custom config
    suite := testing.NewTestSuiteWithConfig("myadapter", engine, config)
    
    // Run comprehensive tests
    t.Run("AllInterfaces", func(t *testing.T) {
        suite.RunAllTests(t)
    })
    
    t.Run("Concurrency", func(t *testing.T) {
        suite.RunConcurrencyTests(t)
    })
}
```

## Available Test Categories

### 1. Interface Tests

Test individual httpx interfaces:

```go
// Test Request interface
requestTester := testing.NewRequestTester(engine)
requestTester.RunAllTests(t)

// Test Responder interface
responderTester := testing.NewResponderTester(engine)
responderTester.RunAllTests(t)

// Test StateStore interface
stateStoreTester := testing.NewStateStoreTester(engine)
stateStoreTester.RunAllTests(t)

// Test Router interface
routerTester := testing.NewRouterTester(engine)
routerTester.RunAllTests(t)

// Test Engine interface
engineTester := testing.NewEngineTester(engine)
engineTester.RunAllTests(t)

// Test Binder interface
binderTester := testing.NewBinderTester(engine)
binderTester.RunAllTests(t)
```

### 2. Middleware Abort Testing

Test middleware abort behavior:

```go
func TestMiddlewareAbort(t *testing.T) {
    engine := New(WithServerAddr(":0"))
    
    // Create abort tracker
    tracker := testing.NewAbortTracker()
    
    // Set up abort testing middleware
    testing.SetupAbortEngine(engine, tracker)
    
    // Test abort behavior
    // The framework will test various abort scenarios
}
```

### 3. Concurrency Testing

Test thread safety and concurrent request handling:

```go
func TestConcurrency(t *testing.T) {
    engine := New(WithServerAddr(":0"))
    suite := testing.NewTestSuite("myadapter", engine)
    
    // Run concurrency tests
    suite.RunConcurrencyTests(t)
}
```

### 4. Performance Benchmarking

Run performance benchmarks:

```go
func BenchmarkMyAdapter(b *testing.B) {
    engine := New(WithServerAddr(":0"))
    suite := testing.NewTestSuite("myadapter", engine)
    
    // Run all benchmarks
    suite.RunBenchmarks(b)
}
```

## Adapter-Specific Examples

### ginx Integration

```go
package ginx

import (
    "testing"
    "github.com/gin-gonic/gin"
    "github.com/go-sphere/httpx/testing"
)

func TestGinxIntegration(t *testing.T) {
    gin.SetMode(gin.TestMode)
    
    engine := New(WithServerAddr(":0"))
    suite := testing.NewTestSuite("ginx", engine)
    
    suite.RunAllTests(t)
}
```

### fiberx Integration

```go
package fiberx

import (
    "testing"
    "github.com/go-sphere/httpx/testing"
    "github.com/gofiber/fiber/v3"
)

func TestFiberxIntegration(t *testing.T) {
    engine := New(WithListen(":0"))
    suite := testing.NewTestSuite("fiberx", engine)
    
    suite.RunAllTests(t)
}
```

## Configuration Options

### TestConfig Structure

```go
type TestConfig struct {
    ServerAddr      string        // Server address (use ":0" for random port)
    RequestTimeout  time.Duration // Timeout for HTTP requests
    ConcurrentUsers int           // Number of concurrent users for concurrency tests
    TestDataSize    int           // Size of test data in bytes
}
```

### Default Configuration

```go
var DefaultTestConfig = TestConfig{
    ServerAddr:      ":0",
    RequestTimeout:  5 * time.Second,
    ConcurrentUsers: 10,
    TestDataSize:    1024,
}
```

## Best Practices

### 1. Test Organization

- Create separate test functions for different test categories
- Use subtests to group related test cases
- Name tests descriptively (e.g., `TestGinxRequestInterface`)

### 2. Configuration Management

- Always use `:0` for random port assignment in tests
- Set appropriate timeouts for CI/CD environments
- Configure adapters for test mode (disable logging, etc.)

### 3. Error Handling

- Check for adapter-specific limitations
- Skip tests for unsupported features
- Provide clear error messages

### 4. Concurrency Testing

- Use appropriate number of concurrent users
- Test thread safety of state management
- Verify request isolation

### 5. Performance Testing

- Run benchmarks separately from functional tests
- Use consistent test data sizes
- Compare results across adapters

## Cross-Adapter Consistency Testing

To verify that all adapters behave consistently:

```go
func TestCrossAdapterConsistency(t *testing.T) {
    adapters := []struct {
        name   string
        engine httpx.Engine
    }{
        {"ginx", ginx.New(ginx.WithServerAddr(":0"))},
        {"fiberx", fiberx.New(fiberx.WithListen(":0"))},
        // Add other adapters...
    }
    
    for _, adapter := range adapters {
        t.Run(adapter.name, func(t *testing.T) {
            suite := testing.NewTestSuite(adapter.name, adapter.engine)
            suite.RunAllTests(t)
        })
    }
}
```

## Troubleshooting

### Common Issues

1. **Port Conflicts**: Always use `:0` for random port assignment
2. **Timing Issues**: Use appropriate timeouts and synchronization
3. **Adapter Limitations**: Check adapter capabilities and skip unsupported tests
4. **Test Isolation**: Reset state between tests, use fresh engines

### Debugging Tips

- Enable verbose logging in test mode
- Use `t.Logf()` to trace test execution
- Check adapter-specific documentation
- Verify httpx interface implementations
- Test with minimal configuration first

## Running Tests

### Run All Tests

```bash
go test ./ginx -v
go test ./fiberx -v
```

### Run Specific Test Categories

```bash
# Run only integration tests
go test ./ginx -run TestIntegration -v

# Run only benchmarks
go test ./ginx -bench=. -v
```

### Run with Race Detection

```bash
go test ./ginx -race -v
```

## Requirements Validation

This integration testing approach validates the following requirements:

- **12.1-12.6**: Adapter integration testing
- **13.1**: Complete test suite coverage
- **13.2**: Concurrency testing
- **13.3**: Performance benchmarking
- **13.4**: Detailed test reporting
- **13.5**: Clear error information

## Contributing

When adding a new adapter:

1. Create an `integration_test.go` file in your adapter package
2. Follow the patterns shown in the examples
3. Test all httpx interfaces
4. Include concurrency and performance tests
5. Document any adapter-specific considerations

## Examples

See the following files for complete examples:

- `ginx/integration_test.go` - ginx adapter integration
- `fiberx/integration_test.go` - fiberx adapter integration
- `testing/integration_examples_test.go` - General patterns and best practices