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

## Server-Sent Events

`httpx.ServerSentEvents` streams SSE responses on every supported framework,
built on the optional `Streamer` capability:

```go
router.GET("/events", func(ctx httpx.Context) error {
    return httpx.ServerSentEvents(ctx, func(w *httpx.SSEWriter) error {
        if err := w.SendJSON("update", payload); err != nil {
            return err // client gone; stop producing
        }
        return w.Send(&httpx.SSEEvent{ID: "42", Event: "done", Data: "bye"})
    })
})
```

Each event is flushed to the client as a single write. Lower-level primitives
are also available: `httpx.AsStreamer` for raw incremental streaming and
`httpx.AsFlusher` for mid-handler flushes (not supported by Fiber).

## Router Feature Detection

`httpx` exposes optional router capability detection through the
`RouterFeatureProvider` interface implemented by every `Router`.

```go
supports := router.SupportsRouterFeature(httpx.RouterFeatureNamedWildcard)
```

Currently supported router feature keys:

- `httpx.RouterFeatureNamedWildcard` - named wildcard params in route path patterns, e.g. `/*filepath`

Feature values are adapter declarations and can be extended in future versions.

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.
(https://github.com/go-sphere/httpx/discussions)
