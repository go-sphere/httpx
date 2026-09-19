# httpx

A unified HTTP framework abstraction layer for Go that provides a consistent interface across multiple popular web frameworks.

## Overview

`httpx` is designed to provide a framework-agnostic HTTP handling layer that allows you to write application logic once and run it on any supported HTTP framework. It currently supports:

- **Gin** (`ginx`) - Fast HTTP web framework
- **Fiber** (`fiberx`) - Express inspired web framework  
- **Echo** (`echox`) - High performance, minimalist framework
- **Hertz** (`hertzx`) - High-performance HTTP framework by CloudWego
- **net/http** (`stdx`) - No web framework at all: the standard library plus a
  small route tree written for this contract. Pulls in no framework and serves
  `Static`/`HandleStd`/std middleware with nothing to bridge; a 10-layer chain
  costs ~35 ns with zero allocations

## Adapter options

Every adapter's `New` takes the same core options under the same names, so
setup code is portable across all five:

| Option | Type | Notes |
| --- | --- | --- |
| `WithAddr(addr)` | `string` | The listen address, on all five. `ginx`/`echox`/`stdx` create the `*http.Server` if one was not supplied; `fiberx` wraps `WithListen`; `hertzx` only applies it when the adapter builds the engine. |
| `WithErrorHandler(h)` | `httpx.ErrorHandler` | Framework-neutral, on all five. `h` is called with a real adapter-backed `httpx.Context`. |
| `WithNativeErrorHandler(h)` | adapter's own `ErrorHandler` | `ginx` and `hertzx` only — the two frameworks whose error-handler shape the adapter installs directly. |
| `WithTrustedProxies(...)` | `...string` | Uniform `ClientIP` policy; an empty list ignores forwarding headers. |
| `WithEngine(e)` | native engine | Bring your own `*gin.Engine`, `*echo.Echo`, `*fiber.App`, `*server.Hertz`. |

Two related names are *not* portable, on purpose:

- `Default<X>ErrorHandler` — each adapter exports `DefaultErrorHandler`, which
  is **that adapter's default handler in its native shape**: the value it
  installs on the framework when you configure nothing. The five signatures
  therefore differ (`func(*gin.Context, error)`, `echo.HTTPErrorHandler`,
  `func(fiber.Ctx, error) error`, `func(context.Context, *app.RequestContext,
  error)`), and for `stdx` the native shape simply *is* `httpx.ErrorHandler`,
  because net/http has no error handler of its own to match.
- `From<Framework>` — `ginx.FromGin`, `echox.FromEcho`, `fiberx.FromFiber`,
  `hertzx.FromHertz` and `stdx.FromStd` build an `httpx.Context` from a native
  one, so httpx helpers can be used from a native error handler or middleware.
  Each takes what its framework hands you.

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
flow (including how httpx layers nest inside the framework's own middleware),
request bodies and binders, forms and uploads, and the edges —
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
composes chains into gin routes, and `ginx/middleware_alloc_test.go` for the
allocation guards).

## Benchmarks

[`benchmarks/BENCHMARK.md`](benchmarks/BENCHMARK.md) compares all five adapters
against the framework they wrap — gin, echo, fiber, hertz and net/http — on the
shared scenario table, plus a fixed-rate network run.

```bash
make bench-report   # every table in one session -> benchmarks/BENCHMARK.md
```

Numbers may only be quoted next to each other when they came out of the same
`bench-report` run: it compiles one test binary and measures every table from
it back to back, recording the environment and the git revision alongside. The
per-table targets (`make bench-adapter`, `bench-suite`, `bench-native`,
`bench-network`) are for iterating on one table, and two of their runs are two
different environments. See [`benchmarks/README.md`](benchmarks/README.md).

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

There is one middleware form. A `httpx.Middleware` receives the rest of the
chain and returns the handler that runs in its place:

```go
type Middleware func(next httpx.Handler) httpx.Handler

func RequestID(next httpx.Handler) httpx.Handler {
    return func(ctx httpx.Context) error {
        ctx.SetContext(withRequestID(ctx.Context()))
        return next(ctx)
    }
}

engine.Use(RequestID)              // engine scope
needAuth := api.Group("/", auth)   // group scope, in one call
```

`Use` is on both `httpx.Engine` and `httpx.Router` (through
`httpx.MiddlewareScope`), and `Group`'s variadic takes the same type.

Until v0.0.5 there were two forms: this one, then called `Interceptor`, and a
`func(httpx.Context) error` that drove the chain with `ctx.Next()`. The
Next-driven form is gone, `Context.Next()` with it, and the composed form took
the `Middleware` name. See the CHANGELOG for the migration.

Four things to know about:

- **Stopping the chain** is returning without calling `next` — after writing a
  response, or by returning an error for the adapter's error handler to render.
  There is no Abort to reason about and no layer index.
- **No per-request state.** The chain is composed into each route at
  registration: one call per layer, zero allocations, and no depth at which cost
  stops growing linearly.
- **What a chain covers.** Every registered route, `Static`, `StaticFS` and
  `HandleStd` included — a layer wraps a static mount and can block it. Plus,
  for an **engine-scope** chain only, the paths no route matched: the adapter's
  404/405 fallback composes the engine's own layers in front of the handler that
  renders the error, so an access log, a panic recovery layer or a CORS layer
  sees a request for `/nope` and may answer it instead. A **group's** layers
  never cover an unmatched path — a 404 belongs to no group.
- **A route's chain is resolved when the route is registered**, not when its
  scope was created, so `engine.Use` after a group exists still reaches the
  routes that group registers afterwards. Routes already registered keep the
  chain they were registered with — the rule every framework applies to its own
  `Use`, and what makes a registered route immutable.
- **An error from an inner layer is rendered at the route**, not at the layer
  that produced it, so a layer that logs the outcome should use the error
  returned by `next`, not only `Context.StatusCode`.

### Native middleware

The framework's own middleware is registered with `UseNative`, on both `Engine`
and `Router` in `ginx` (`gin.HandlerFunc`), `echox` (`echo.MiddlewareFunc`),
`fiberx` (`fiber.Handler`) and `hertzx` (`app.HandlerFunc`):

```go
router.(*ginx.Router).UseNative(gin.Recovery())
```

There is no `Adapt<Framework>Middleware` any more, and there cannot be: every
httpx layer on a scope shares a single native handler slot, so a native
middleware wrapped as an `httpx.Middleware` would advance the framework's own
index past that slot instead of into the httpx chain. `UseNative` was already
the documented preference; it is now the only way.

A `UseNative` layer therefore always runs *outside* everything registered with
`Use` on the same scope, whatever the call order. On `fiberx` one extra rule
applies: register it before the routes it should wrap, because fiber matches its
route stack in registration order.

`stdx` has no `UseNative`, deliberately. There is no framework chain to hand a
middleware to, and net/http has no middleware type but the one
`AdaptStdMiddleware` already takes.

### Plain net/http middleware

`AdaptStdMiddleware(func(http.Handler) http.Handler) httpx.Middleware` is on all
five adapters and works the same way on each: request mutation (including
context values), response-writer wrapping and short-circuiting all propagate.
`func(http.Handler) http.Handler` is framework-neutral, so unlike a native
middleware it maps directly onto the composed form.

```go
router.Use(ginx.AdaptStdMiddleware(otelhttp.NewMiddleware("api")))
```

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