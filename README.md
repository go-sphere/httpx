# httpx

A unified HTTP framework abstraction layer for Go that provides a consistent interface across multiple popular web frameworks.

## Overview

`httpx` is designed to provide a framework-agnostic HTTP handling layer that allows you to write application logic once and run it on any supported HTTP framework. It currently supports:

- **Gin** (`ginx`) - Fast HTTP web framework
- **Fiber** (`fiberx`) - Express inspired web framework  
- **Echo** (`echox`) - High performance, minimalist framework
- **Hertz** (`hertzx`) - High-performance HTTP framework by CloudWego

## Testing

The project includes comprehensive testing infrastructure in two packages:

- **`testing/`** - Reusable test utilities and framework-agnostic test suite
- **`integration/`** - Cross-framework integration tests with flexible execution modes

### Test Execution Modes

Three execution modes support different testing workflows:

- **Individual Mode** - Run each interface test separately for detailed debugging
- **Batch Mode** - Fast comprehensive validation for CI/CD pipelines
- **Benchmark Mode** - Performance testing and regression detection

Run tests:
```bash
# Run all integration tests
go test ./integration/... -v

# Run with coverage
go test ./integration/... -cover

# Run benchmarks
go test -bench=. ./integration/...
```

For more details, see [Testing Documentation](specs/001-test-suite-optimization/quickstart.md).

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.
(https://github.com/go-sphere/httpx/discussions)