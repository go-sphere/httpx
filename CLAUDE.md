# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Repository Layout

This is a Go workspace (`go.work`) containing **six modules**, each with its own `go.mod`:

- `.` (root, `github.com/go-sphere/httpx`) — framework-agnostic interfaces (`Context`, `Engine`, `Router`, `Binder`, `Responder`, `Error`) plus helpers like `FixWildcardPathIfNeed`, `ListenAndAutoShutdown`, `WithJson`. The root module has **no third-party dependencies** — keep it that way.
- `ginx/`, `fiberx/`, `echox/`, `hertzx/` — adapter modules that implement the root interfaces over Gin, Fiber v3, Echo v4, and Hertz respectively. Each is independently versioned.
- `conformance/` — the single test suite. It uses `replace` directives to point at the local adapter modules and runs the same scenarios across all four adapters.

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

`StateStore.Set/Get` is **not** propagated through `Context.Context()`. To pass values into downstream goroutines/RPC, middleware must call `SetContext(context.WithValue(...))`. Document this when touching state plumbing.

Binder validation is part of the contract: after a successful decode into a struct, every adapter runs go-playground/validator rules declared with the `binding` tag (gin's convention); failures surface as 400 via `WrapBindError`. Each adapter also has `WithTrustedProxies(...)` for a uniform ClientIP trusted-proxy policy (empty list = ignore forwarding headers) and `AdaptStdMiddleware(func(http.Handler) http.Handler)` to mount plain net/http middleware (request mutation, writer wrapping, and short-circuiting all propagate). Wildcard registration is validated by `httpx.ValidateWildcardPath` — one named wildcard, final segment — and panics uniformly otherwise.

`ResponseInfo` (StatusCode) is part of the `Context` interface; `httpx.AsResponseInfo` remains only for backward compatibility. Optional capabilities are exposed as separate interfaces probed via type assertion:
- `NativeContextProvider` → `httpx.AsNativeContext[T](ctx)` (escape hatch to the underlying `*gin.Context`, `fiber.Ctx`, etc.)
- `StdHandlerMounter` → `httpx.MountStd(r, method, path, h)` mounts a plain `net/http` handler
- `TestRequester` → `httpx.AsTestRequester(engine)` serves a request in-process for tests
- `RouterFeatureProvider.SupportsRouterFeature(...)` → currently only `RouterFeatureNamedWildcard`. Adapters without named wildcards (echox, fiberx) normalize `/*filepath` internally at registration time and keep `Param("filepath")` working; `FixWildcardPathIfNeed`/`WildcardParamName` are the shared helpers behind this.

### Errors

`httpx.Error` composes `StatusError + CodeError + MessageError`. The concrete type is unexported on purpose — return the `Error` interface from constructors. `ParseError(err)` extracts those fields. Adapter default error handlers use `RenderError` / `ClassifyError` (status + `{success, code, message}`, no raw `err.Error()`). `NewXxxError(msg)` puts `msg` in `GetMessage()`; `XxxError(err)` without extra arguments does not. Bind failures should go through `WrapBindError`.

`httpx.ErrorHandler` (`func(Context, error)`) is the framework-neutral error handler. Every adapter accepts it — `ginx.WithHTTPXErrorHandler`, `hertzx.WithHTTPXErrorHandler`, `echox.WithErrorHandler`, `fiberx.WithErrorHandler` — and invokes it with a real adapter-backed `httpx.Context`. Each adapter also exports its native default (`ginx.DefaultErrorHandler`, `fiberx.DefaultErrorHandler`, `echox.DefaultHTTPErrorHandler`, `hertzx.DefaultErrorHandler`); the echox/fiberx defaults understand `*echo.HTTPError`/`*fiber.Error` so framework 404s keep their status. A committed response is never overwritten by an error body on any adapter.

### Adapter pattern

Each adapter follows the same shape: `engine.go` (Config/Option/New/Start/Stop/IsRunning), `router.go` (RouterGroup wrapper with `toXxxHandler` that calls the framework handler and routes errors through the adapter's `ErrorHandler`), `context.go` (concrete `Context` impl), `middleware.go` (`adaptMiddlewares` bridges `httpx.Middleware` → native middleware), and where the native binder is insufficient, a `uri_binding.go` / `binding.go`.

When adding behavior, **change the root interface first, then implement it in all four adapters, then add a conformance test**. The conformance suite (`conformance/assertions_test.go`) treats `ginx` as the baseline and asserts the other three produce equivalent status / Content-Type / body / `Location` / `X-Trace` / `Set-Cookie`. If Gin's behavior is itself wrong, fix Gin's adapter — don't loosen the assertions.

### Engine lifecycle

`Engine.Start()` blocks; `Stop(ctx)` triggers graceful shutdown; `IsRunning()` is backed by `atomic.Bool`. The root package also provides `ListenAndAutoShutdown` for context-driven shutdown of a plain `*http.Server`.
