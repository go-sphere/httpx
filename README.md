# httpx

A unified HTTP framework abstraction layer for Go that provides a consistent interface across multiple popular web frameworks.

## Overview

`httpx` is designed to provide a framework-agnostic HTTP handling layer that allows you to write application logic once and run it on any supported HTTP framework. It currently supports:

- **Gin** (`ginx`) - Fast HTTP web framework
- **Fiber** (`fiberx`) - Express inspired web framework  
- **Echo** (`echox`) - High performance, minimalist framework
- **Hertz** (`hertzx`) - High-performance HTTP framework by CloudWego
- **net/http** (`stdx`) - No web framework at all: the standard library plus a
  small route tree written for this contract. Pulls in no framework, serves
  `Static`/`HandleStd`/std middleware with nothing to bridge, and composes
  `Middleware` at registration like `Interceptor`, so a 10-layer chain costs
  ~61 ns with zero allocations

## Testing

The shared conformance suite lives in `httpxtest`. An adapter is certified on
its own — against a recorded contract rather than against another adapter — by
declaring its capabilities and an engine factory:

```go
func TestConformance(t *testing.T) {
    httpxtest.Run(t, httpxtest.Suite{
        Name: "ginx",
        Caps: httpxtest.Caps{NamedWildcard: true, Flusher: true},
        NewEngine: func(tb testing.TB, opts httpxtest.Options) httpx.Engine {
            return ginx.New(ginx.WithEngine(gin.New()))
        },
    })
}
```

A third-party adapter can import `httpxtest` and certify itself against the
same contract as the official four: registration and routing, chain control
flow (including mixing `Middleware`, `Interceptor` and the framework's own
middleware), request bodies and binders, forms and uploads, and the edges —
repeated query/header keys, unknown-length bodies, encoded paths, large
bodies. Every capability an adapter declares in `Caps` is also checked against
what it actually does. The four official suites are wired in
`conformance/httpxtest_suite_test.go` for now and move into each adapter's own
module once the root module is released with `httpxtest` in it (an adapter
cannot import a package that its declared `httpx` version does not contain, and
adapter `go.mod` files must stay free of `replace`). Response shape is pinned by the golden
contracts in `httpxtest/golden/`, which every adapter compares against; see
that directory's README for the format. Rewrite them with `make golden`, which
records from each adapter and then verifies that all of them still agree.

```bash
make test          # every module, including each adapter's conformance run
make golden        # rewrite and verify the shared response contracts
go test ./conformance -run TestHTTPXTestSuite/ginx -v

# The same scenario table, measured per adapter through its own dispatcher.
go test ./conformance -run '^$' -bench '^BenchmarkHTTPXTestSuite$/ginx' -benchmem
```

`conformance/` remains for what genuinely needs all four frameworks in one
process: the cross-framework benchmark tables and the cases that reach for
each framework's native middleware. Adapter-specific behavior is tested in the
adapter's own module (for example `ginx/middleware_test.go` for how ginx
registers middleware on gin, and `ginx/interceptor_test.go` for composed
chains).

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

## Middleware

`Router.Use` and `Router.Group` take `httpx.Middleware` (`func(httpx.Context) error`).
A middleware continues the chain with `ctx.Next()` and stops it by returning
without calling `Next` — either after writing a response, or by returning an
error for the adapter's error handler to render.

Each middleware is registered as one native handler, so it interleaves with
native middleware in registration order and `Next` behaves exactly as the
framework's own does. That costs one wrapper allocation and two calls per layer
per request; `Interceptor` below is the cheap path for chains where that
matters.

Native middleware needs no special handling — it occupies its own handler slot
like every other layer — but `UseNative` skips the adapter entirely:

```go
// Preferred: no adapter in between, and the position is explicit.
router.(*ginx.Router).UseNative(gin.Recovery())

// Equivalent behavior, one adapter layer more.
router.Use(ginx.AdaptGinMiddleware(gin.Recovery()))
```

`AdaptStdMiddleware` (`func(http.Handler) http.Handler`) drives the chain
through `ctx.Next()` and works the same way.

### Interceptors (experimental)

`httpx.Interceptor` is an additive second form that takes the rest of the
chain instead of driving it through `ctx.Next()`:

```go
func RequestID(next httpx.Handler) httpx.Handler {
    return func(ctx httpx.Context) error {
        ctx.SetContext(withRequestID(ctx.Context()))
        return next(ctx)
    }
}

httpx.UseInterceptor(router, RequestID)   // reports whether the scope composed them
```

Because the chain is composed into each route at registration, it needs no
per-request object: one call per layer, zero allocations, and no depth at
which cost stops growing linearly. `ginx` composes natively; the other
adapters fall back to adapting each layer, which behaves identically and costs
what `Use` costs. `httpx.AsMiddleware` and `httpx.AsInterceptor` convert
between the two forms. `AsInterceptor` allocates continuation state per request
and an additional wrapper when the context exposes optional capabilities such
as streaming, flushing, or native context access; those capabilities are
preserved by the conversion.

Three things to know about:

- Interceptors always run inside anything registered with `Use`/`UseNative` on
  the same scope, regardless of call order.
- They are composed into **registered routes**, which includes `Static`,
  `StaticFS` and `HandleStd` — an interceptor wraps a static mount and can
  block it — but not unmatched paths. An engine-wide concern that must also
  cover 404s (access log, panic recovery) belongs on `Use`.
- An error from an inner layer is rendered at the route rather than at the
  layer that produced it, so a layer that logs the outcome should use the error
  returned by `next`, not only `Context.StatusCode`.

On a bare chain the form is worth 2x versus `Middleware` (see the benchmarks);
over real middleware it is worth a few percent, because the middlewares' own
work dominates.

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

### 发布多个模块

根模块和 adapter 必须按顺序发布。开发时 `go.work` 使用本地根模块，不能证明 adapter 的已发布依赖足以编译。

1. 完成 `make check`，提交根模块的变更后发布根模块 `v0.0.5`。
2. 执行 `make prepare-release TAG=v0.0.5`，更新五个 adapter 的 `go.mod`/`go.sum`。根版本不可下载时，此命令在修改文件前退出。
3. 检查并提交依赖更新，再执行 `make release-check TAG=v0.0.5`。此步骤关闭 workspace，逐个验证消费者实际使用的依赖和测试。
4. 执行 `make tag-all TAG=v0.0.5` 发布 adapter tags；它会先运行上述发布检查，依赖仍指向旧版本时拒绝打 tag。

`make prepare-release` 和 `make release-check` 不创建或推送 tag。
