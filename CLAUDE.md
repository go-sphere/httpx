# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Repository Layout

This is a Go workspace (`go.work`) containing **six modules**, each with its own `go.mod`:

- `.` (root, `github.com/go-sphere/httpx`) — framework-agnostic interfaces (`Context`, `Engine`, `Router`, `Binder`, `Responder`, `Error`) plus helpers like `FixWildcardPathIfNeed`, `ListenAndAutoShutdown`, `WithJson`. The root module has **no third-party dependencies** — keep it that way.
- `ginx/`, `fiberx/`, `echox/`, `hertzx/` — adapter modules that implement the root interfaces over Gin, Fiber v3, Echo v4, and Hertz respectively. Each is independently versioned.
- `conformance/` — the single test suite. It uses `replace` directives to point at the local adapter modules and runs the same scenarios across all four adapters.

`go.work` ties them together for local development; tags are published per-module as `ginx/vX.Y.Z`, `fiberx/vX.Y.Z`, etc. (see `make tag-all`).

## Common Commands

All commands run from the repo root.

```bash
make test          # go test ./conformance/... -v
make bench         # framework benchmarks
make bench-5x      # benchmarks with -count=5
make lint          # for each module: go fix, fmt, vet, get, test, mod tidy, golangci-lint, nilaway
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

`StateStore.Set/Get` is **not** propagated through `Context.Context()`. To pass values into downstream goroutines/RPC, middleware must call `SetContext(context.WithValue(...))`. Document this when touching state plumbing.

Optional capabilities are exposed as separate interfaces probed via type assertion:
- `ResponseInfo` → `httpx.AsResponseInfo(ctx)`
- `NativeContextProvider` → `httpx.AsNativeContext[T](ctx)` (escape hatch to the underlying `*gin.Context`, `fiber.Ctx`, etc.)
- `RouterFeatureProvider.SupportsRouterFeature(...)` → currently only `RouterFeatureNamedWildcard`. Use `FixWildcardPathIfNeed` to handle adapters that lack named wildcards (rewrites `/*filepath` → `/*` and returns `"*"` as the param key).

### Errors

`httpx.Error` composes `StatusError + CodeError + MessageError`. The concrete type is unexported on purpose — return the `Error` interface from constructors. `ParseError(err)` extracts those fields. Adapter default error handlers use `RenderError` / `ClassifyError` (status + `{success, code, message}`, no raw `err.Error()`). `NewXxxError(msg)` puts `msg` in `GetMessage()`; `XxxError(err)` without extra arguments does not. Bind failures should go through `WrapBindError`.

### Adapter pattern

Each adapter follows the same shape: `engine.go` (Config/Option/New/Start/Stop/IsRunning), `router.go` (RouterGroup wrapper with `toXxxHandler` that calls the framework handler and routes errors through the adapter's `ErrorHandler`), `context.go` (concrete `Context` impl), `middleware.go` (`adaptMiddlewares` bridges `httpx.Middleware` → native middleware), and where the native binder is insufficient, a `uri_binding.go` / `binding.go`.

When adding behavior, **change the root interface first, then implement it in all four adapters, then add a conformance test**. The conformance suite (`conformance/assertions_test.go`) treats `ginx` as the baseline and asserts the other three produce equivalent status / Content-Type / body / `Location` / `X-Trace` / `Set-Cookie`. If Gin's behavior is itself wrong, fix Gin's adapter — don't loosen the assertions.

### Engine lifecycle

`Engine.Start()` blocks; `Stop(ctx)` triggers graceful shutdown; `IsRunning()` is backed by `atomic.Bool`. The root package also provides `ListenAndAutoShutdown` for context-driven shutdown of a plain `*http.Server`.
