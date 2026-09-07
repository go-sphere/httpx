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

## Streaming and Server-Sent Events

Every official adapter implements the optional `Streamer` context capability.
Use `AsStreamer` when you need raw incremental output such as NDJSON:

```go
router.GET("/chunks", func(ctx httpx.Context) error {
    streamer, ok := httpx.AsStreamer(ctx)
    if !ok {
        return httpx.ErrStreamerNotSupported
    }
    return streamer.Stream(http.StatusOK, "application/x-ndjson", func(w io.Writer) error {
        for _, chunk := range chunks {
            if _, err := fmt.Fprintf(w, "%s\n", chunk); err != nil {
                return err // client gone; stop producing
            }
        }
        return nil
    })
})
```

Each write made by the stream callback is flushed to the client. Once streaming
starts, the response status and headers are committed, so callback errors are
for termination and cleanup; they cannot become a different HTTP response.

`ServerSentEvents` is the framework-neutral SSE layer over `Streamer`:

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

`SSEWriter` supports `Send`, `SendData`, `SendJSON`, and `Comment`. Each event
is encoded and flushed as one unit. `ServerSentEvents` sets
`Content-Type: text/event-stream`, `Cache-Control: no-cache`, and
`X-Accel-Buffering: no`; stop producing as soon as a send fails or the request
context is canceled.

Streaming capability matrix:

| Capability | Gin | Echo | Hertz | Fiber |
| --- | --- | --- | --- | --- |
| `Streamer` / `ServerSentEvents` | Yes | Yes | Yes | Yes |
| `Flusher` / `AsFlusher` | Yes | Yes | Yes | No |

Fiber uses its deferred stream-writer API, so the stream callback runs after
the handler returns and `Stream` returns `nil` immediately. In-process requests
through `httpx.TestRequester` buffer stream writes into the final response body;
use a real HTTP connection when testing incremental delivery or disconnects.

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
