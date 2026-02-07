# httpx

A unified HTTP framework abstraction layer for Go that provides a consistent interface across multiple popular web frameworks.

## Overview

`httpx` is designed to provide a framework-agnostic HTTP handling layer that allows you to write application logic once and run it on any supported HTTP framework. It currently supports:

- **Gin** (`ginx`) - Fast HTTP web framework
- **Fiber** (`fiberx`) - Express inspired web framework  
- **Echo** (`echox`) - High performance, minimalist framework
- **Hertz** (`hertzx`) - High-performance HTTP framework by CloudWego

## Testing

The project provides a single conformance test suite under `conformance/`.
It uses `ginx` as the baseline behavior and checks that other adapters
(`fiberx`, `echox`, `hertzx`) match it.

Run tests:
```bash
# Run conformance tests
go test ./conformance/... -v

# Run with coverage
go test ./conformance/... -cover
```

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.
(https://github.com/go-sphere/httpx/discussions)
