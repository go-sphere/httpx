# httpx Testing Framework

This package provides a unified testing framework for httpx adapters, ensuring consistent behavior across all Web framework implementations (ginx, fiberx, echox, fasthttpx, hertzx).

## Package Structure

- `utils.go` - Core utility functions for testing
- `config.go` - Test configuration and error types
- `utils_test.go` - Tests for utility functions
- `config_test.go` - Tests for configuration and error types

## Core Components

### Utility Functions

- `EqualSlices[T comparable](a, b []T) bool` - Compares two slices for equality
- `MakeRequest(method, url string, body io.Reader, headers map[string]string) (*http.Request, error)` - Creates HTTP requests
- `AssertResponse(t *testing.T, resp *http.Response, expectedCode int, expectedBody string)` - Validates HTTP responses
- `AssertHeader(t *testing.T, resp *http.Response, key, expectedValue string)` - Validates response headers
- `AssertCookie(t *testing.T, resp *http.Response, name, expectedValue string)` - Validates response cookies

### Configuration

- `TestConfig` - Configuration structure for test parameters
- `DefaultTestConfig` - Default configuration values
- `TestStruct` - Standard structure for binding tests
- `NestedTestStruct` - Complex structure for advanced binding tests

### Error Handling

- `TestError` - Structured error type with detailed context
- `NewTestError()` - Constructor for test errors

## Usage

This package is designed to be imported by httpx adapter packages for standardized testing:

```go
import "github.com/go-sphere/httpx/testing"
```

The framework supports Go workspace development mode and is configured to work with all httpx adapters.