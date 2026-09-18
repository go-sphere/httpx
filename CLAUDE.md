# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Repository Layout

This is a Go workspace (`go.work`) containing **six modules**, each with its own `go.mod`:

- `.` (root, `github.com/go-sphere/httpx`) — framework-agnostic interfaces (`Context`, `Engine`, `Router`, `Binder`, `Responder`, `Error`) plus helpers like `FixWildcardPathIfNeed`, `ListenAndAutoShutdown`, `WithJson`. The root module has **no third-party dependencies** — keep it that way.
- `ginx/`, `fiberx/`, `echox/`, `hertzx/` — adapter modules that implement the root interfaces over Gin, Fiber v3, Echo v4, and Hertz respectively. Each is independently versioned.
- `httpxtest/` — the shared conformance suite (part of the root module, stdlib-only). An adapter supplies a `Suite` (name, `Caps`, an engine factory, and two optional hooks: `StdMiddleware` for `AdaptStdMiddleware` and `NativeMiddleware` for the framework's own middleware — both are needed because neither is reachable through the `Router` interface) and calls `httpxtest.Run`. Case groups: `Middleware`/`Router`/`Static` (registration), `Chain` (order, mixing `Middleware`+`Interceptor`+native, depth, stopping, error propagation), `Caps` (each capability claim is verified against actual behavior, so `Caps` cannot drift into documentation), `Request` (bodies, five binders, forms, uploads, cookies, request info), `RequestEdges` (repeated query/header keys, unknown-length body, encoded and non-ASCII paths, 1 MiB bodies), `Response`/`Context` (every responder, `WithJson`, the standard context and its separation from the state store), `Regression` (cases that exist because an adapter once got them wrong), `Contract` (validation, method-name case, registration-time panics, sniffed content types, state-store nil, plain net/http middleware), `Stream`/`ErrorHandling`, and `Scenarios` (the shared `Scenario` table — declarative method/target/header/body so a benchmark can reuse one request and rewind its body without allocating; `httpxtest.RunBenchmarks` measures the same table, so adding a scenario adds both a contract and a benchmark, and `BenchmarkOnly` marks the ones whose response is too large to record). Chain order is asserted twice on purpose: the handler returns it in the body so the golden makes every adapter agree, and the case states the full sequence inline so regenerating a golden cannot accept a reordering. `Suite.Dispatch` is what makes the shared benchmarks measure the adapter rather than the harness: it drives the framework's own dispatcher with reusable buffers (~20 lines per adapter, in `conformance/httpxtest_suite_test.go`). Without it `RunBenchmarks` falls back to `httpx.TestRequester`, which allocates 20+ objects and a few microseconds per request — 98% of an empty request's measurement. Allocation guards also stay in each adapter's module for the same reason (`ginx/interceptor_test.go`).
- `conformance/` — what genuinely cannot move into `httpxtest`: the cross-framework benchmark tables (`BenchmarkAdapter`, `BenchmarkFramework`, and `BenchmarkNativeVsHTTPX` — which pairs every shared scenario with a hand-written implementation on the raw framework, because "how you would write this without httpx" cannot be generated from the shared table and has to live next to the framework imports), the four suite definitions (until the root module ships `httpxtest`), engine lifecycle and anything needing a **real listener** (trusted proxies/`ClientIP`, incremental stream delivery, FD leaks — the in-process requesters of fiberx/hertzx report `0.0.0.0` as the peer, so a proxy policy cannot be verified through them), the concurrency stress test, and the cases that assert a specific framework's native context type. It uses `replace` directives to point at the local adapter modules. Gin-only diagnostics live in `ginx/` (`call_depth_benchmark_test.go`, `middleware_depth_benchmark_test.go`, `middleware_chain_benchmark_test.go`, `middleware_model_benchmark_test.go`). The golden machinery now lives only in `httpxtest`.

`go.work` overlays the six modules for local and GitHub Actions builds. Adapter `go.mod` files must **not** contain `replace` (that would ship in `ginx/vX.Y.Z` and break consumers). CI sets `GOWORK` to the repo-root `go.work`. Tags are published per-module as `ginx/vX.Y.Z`, `fiberx/vX.Y.Z`, etc. (see `make tag-all`).

## Common Commands

All commands run from the repo root.

```bash
make deps-update   # update direct dependencies in all six modules
make fmt           # format all modules
make test          # test all modules through go.work
make test-race     # test all modules with the race detector
make bench         # framework benchmarks
make bench-5x      # benchmarks with -count=5
make lint          # non-mutating format, vet, golangci-lint, and nilaway checks
make check         # dependency checks, lint, and race-enabled tests
make tag-all TAG=v0.0.4   # tag every adapter at once
```

Run a single conformance test:
```bash
go test ./conformance/ -run TestEngineConformance/ginx -v
```

Work inside a specific adapter (each is a separate module):
```bash
cd ginx && go test ./...
```

## Architecture

### The Context contract

`httpx.Context` (in `context.go`) is a composite of four sub-interfaces — `Request` (info + body + form), `Responder`, `Binder`, and `StateStore` — plus `Context()`/`SetContext()` for the standard `context.Context` and `Next()` for chain control. The doc comments on each sub-interface define **side-effect contracts** (e.g. `RequestInfo` methods must not consume the body; `BodyAccess`/`FormAccess` may). When adding methods, preserve these guarantees in every adapter or conformance will diverge.

`BodyRaw` returns a slice the **caller owns**: it must stay valid and unchanged after the handler returns. The fasthttp-based adapters therefore copy out of the pooled request buffer (`bytes.Clone`), which costs one allocation the size of the body — 1 MiB body: fiberx 606ns → 123µs, hertzx 20µs → 113µs. `BodyReader` and the native context are the zero-copy paths. This class of bug is invisible to the shared suite (every in-process requester builds a fresh native context per request, so nothing is ever reused); the deterministic cases live in `conformance/body_ownership_conformance_test.go`, which drives fasthttp/hertz context reuse directly.

`StateStore.Set/Get` is **not** propagated through `Context.Context()`. To pass values into downstream goroutines/RPC, middleware must call `SetContext(context.WithValue(...))`. Document this when touching state plumbing.

Binder validation is part of the contract: after a successful decode into a struct, every adapter runs go-playground/validator rules declared with the `binding` tag (gin's convention); failures surface as 400 via `WrapBindError`. Each adapter also has `WithTrustedProxies(...)` for a uniform ClientIP trusted-proxy policy (empty list = ignore forwarding headers) and `AdaptStdMiddleware(func(http.Handler) http.Handler)` to mount plain net/http middleware (request mutation, writer wrapping, and short-circuiting all propagate). Wildcard registration is validated by `httpx.ValidateWildcardPath` — one named wildcard, final segment — and panics uniformly otherwise.

`ResponseInfo` (StatusCode) is part of the `Context` interface; `httpx.AsResponseInfo` remains only for backward compatibility. Optional capabilities are exposed as separate interfaces probed via type assertion:
- `NativeContextProvider` → `httpx.AsNativeContext[T](ctx)` (escape hatch to the underlying `*gin.Context`, `fiber.Ctx`, etc.)
- `StdHandlerMounter` → `httpx.MountStd(r, method, path, h)` mounts a plain `net/http` handler
- `TestRequester` → `httpx.AsTestRequester(engine)` serves a request in-process for tests
- `Flusher` → `httpx.AsFlusher(ctx)` flushes buffered response data mid-handler (ginx/echox/hertzx; fiberx cannot and does not implement it)
- `Streamer` → `httpx.AsStreamer(ctx)` incremental streaming/SSE on all four adapters (on fiberx the callback runs after the handler returns). `httpx.ServerSentEvents(ctx, fn)` is the SSE layer on top: it sets `Cache-Control: no-cache` / `X-Accel-Buffering: no`, commits a 200 `text/event-stream` response, and hands fn an `*httpx.SSEWriter` (`Send`/`SendData`/`SendJSON`/`Comment`) that encodes WHATWG event-stream framing in `sse.go` and flushes one event per write.
- `RouterFeatureProvider.SupportsRouterFeature(...)` → currently only `RouterFeatureNamedWildcard`. Adapters without named wildcards (echox, fiberx) normalize `/*filepath` internally at registration time and keep `Param("filepath")` working; `FixWildcardPathIfNeed`/`WildcardParamName` are the shared helpers behind this.

### Errors

`httpx.Error` composes `StatusError + CodeError + MessageError`. The concrete type is unexported on purpose — return the `Error` interface from constructors. `ParseError(err)` extracts those fields. Adapter default error handlers use `RenderError` / `ClassifyError` (status + `{success, code, message}`, no raw `err.Error()`). `NewXxxError(msg)` puts `msg` in `GetMessage()`; `XxxError(err)` without extra arguments does not. Bind failures should go through `WrapBindError`.

`httpx.ErrorHandler` (`func(Context, error)`) is the framework-neutral error handler. Every adapter accepts it — `ginx.WithHTTPXErrorHandler`, `hertzx.WithHTTPXErrorHandler`, `echox.WithErrorHandler`, `fiberx.WithErrorHandler` — and invokes it with a real adapter-backed `httpx.Context`. Each adapter also exports its native default (`ginx.DefaultErrorHandler`, `fiberx.DefaultErrorHandler`, `echox.DefaultHTTPErrorHandler`, `hertzx.DefaultErrorHandler`); the echox/fiberx defaults understand `*echo.HTTPError`/`*fiber.Error` so framework 404s keep their status. A committed response is never overwritten by an error body on any adapter — and the error is not lost either: an outer layer's `ctx.Next()` still returns it, so logging and metrics see a handler that failed after writing its response. Each adapter keeps it on its own path (gin/hertz append to the native error list, echo returns it to echo's error path, fiberx parks it on the context under an unexported Locals key because returning it to fiber would let fiber's ErrorHandler render over the committed body — that lookup costs ~4-6ns per `Use` layer and nothing on the `Interceptor` path, which never crosses a native handler boundary). Pinned by `Regression/OuterLayerSeesErrorAfterCommittedResponse`.

Every adapter defaults its `httpx.ErrorHandler` so httpx errors are rendered by the adapter, not by the framework's own handler. That matters for the one configuration an adapter cannot fix up afterwards: `fiber.Config` is immutable after `fiber.New`, so an engine passed through `fiberx.WithEngine` keeps the caller's `ErrorHandler` (fiber's plain-text default, which leaks `err.Error()` and reports every status as 500) and the caller's `UnescapePath` (off by default, which leaves route parameters percent-encoded). `fiberContext.paramValue`/`bindURI` decode when that flag is off — both gated so an ordinary request pays only a byte scan. The whole suite runs a second time against a caller-built `fiber.App` (`fiberxOwnEngineSuite` in `conformance/httpxtest_suite_test.go`), which is how both defects surfaced; add to it rather than trusting that a constructor-built engine represents every deployment.

### Adapter pattern

Each adapter follows the same shape: `engine.go` (Config/Option/New/Start/Stop/IsRunning), `router.go` (RouterGroup wrapper with `toXxxHandler` that calls the framework handler and routes errors through the adapter's `ErrorHandler`), `context.go` (concrete `Context` impl), `middleware.go` (`adaptMiddlewares` bridges `httpx.Middleware` → native middleware), and where the native binder is insufficient, a `uri_binding.go` / `binding.go`.

`interceptor.go` (root) adds the **experimental** composed middleware form: `Interceptor = func(next Handler) Handler`, the optional `InterceptorScope` capability (`UseInterceptor`), `ComposeInterceptors`, and the `AsMiddleware`/`AsInterceptor` converters. `ginx.Router.UseInterceptor` stores the layers and composes them into each route inside `toGinHandler`, so a composed chain holds no per-request state at all (0 allocations, one call per layer, no depth cliff). Consequences that are part of the contract and are pinned by `ginx/interceptor_test.go` plus `conformance/middleware_chain_conformance_test.go`; `ginx/middleware_test.go` covers the `Use` path's registration behavior: interceptors always run inside middleware registered through `Use`/`UseNative` on the same scope whatever the call order, the chain is snapshotted per route at registration, returning without calling `next` is how a layer stops the chain (no Abort bookkeeping), and an inner error is rendered at the route — not at the failing layer, unlike `Use`. Adapters that do not implement `InterceptorScope` get the `AsMiddleware` fallback through `httpx.UseInterceptor`. `Static`/`StaticFS`/`HandleStd` register through `Handle` and therefore *are* inside interceptor chains: all four adapters serve static mounts with the shared `httpx.StaticFileHandler` (net/http `FileServer` + `StripPrefix`, 404 for a directory or the bare prefix) wrapped in an adapter-local `stdLeaf` that serves a plain `http.Handler` from the native context. Unmatched paths are still outside any interceptor.

When adding behavior, **change the root interface first, then implement it in all four adapters, then add a conformance test**. Response shape is pinned by golden contracts (`conformance/golden_test.go`, files in `conformance/testdata/golden/`): `assertMatchesGolden` renders status / Content-Type / `Location` / `X-Trace` / `Set-Cookie` / body (JSON canonicalized) into one text snapshot, requires **all four** adapters to agree, and compares the agreed value against the recorded file. Regenerate with `go test ./conformance -run TestXxx -update-golden`; update mode refuses to write while adapters disagree, so a golden file is a four-way consensus, never a recording of ginx. This replaced an older `assertMatchesGin` helper that defined correctness as "equal to whatever ginx produced" — that hid two classes of defect: a wrong ginx behavior, and a change in the shared root package (`RenderError`, `WrapBindError`, the JSON envelope) moving all four responses at once. Content-Type is part of the contract only where the body is (see the rule in `contractOf`); everything that is *not* response shape — middleware order, abort, allocation counts, lifecycle, streaming timing — stays as inline assertions, because a golden file for a control-flow sequence invites `-update-golden` to accept a reordering bug. If an adapter's behavior is itself wrong, fix that adapter — don't loosen the contract.

### Engine lifecycle

`Engine.Start()` blocks; `Stop(ctx)` triggers graceful shutdown; `IsRunning()` is backed by `atomic.Bool`. Engines are **single-use**: Start after Stop (in either order) returns `httpx.ErrEngineClosed` on every adapter — construct a new Engine to serve again. ginx/echox bind the listener before setting the running flag. The root package also provides `ListenAndAutoShutdown` for context-driven shutdown of a plain `*http.Server`.
