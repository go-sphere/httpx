# Changelog

## v0.0.5

The first release of `stdx`, and the release where the five adapters stopped
disagreeing. Thirteen planned changes plus eight found by a pre-release review;
seven of the twenty-one are cases where the same call returned different
answers per framework.

This release is **breaking**. Everything below that changes a signature or
removes a symbol is listed under Breaking changes, with what to write instead.

### Breaking changes

**`Responder.DataFromReader` takes `size int64`**

```go
DataFromReader(code int, contentType string, r io.Reader, size int) error    // v0.0.4
DataFromReader(code int, contentType string, r io.Reader, size int64) error  // v0.0.5
```

Every framework and `Content-Length` itself are `int64`; the old doc carried a
"limited to 2GiB on 32-bit" caveat and callers were writing `int(size)`.
Fiber's `SendStream` and hertz's `SetBodyStream` still take `int`, so a length
that does not fit degrades to `-1` (chunked) rather than truncating into a
`Content-Length` that does not match the body.

**One error-handler option name across all five adapters**

| v0.0.4 | v0.0.5 |
| --- | --- |
| `ginx.WithHTTPXErrorHandler(httpx.ErrorHandler)` | `ginx.WithErrorHandler(httpx.ErrorHandler)` |
| `hertzx.WithHTTPXErrorHandler(httpx.ErrorHandler)` | `hertzx.WithErrorHandler(httpx.ErrorHandler)` |
| `ginx.WithErrorHandler(ginx.ErrorHandler)` | `ginx.WithNativeErrorHandler(ginx.ErrorHandler)` |
| `hertzx.WithErrorHandler(hertzx.ErrorHandler)` | `hertzx.WithNativeErrorHandler(hertzx.ErrorHandler)` |
| `echox.DefaultHTTPErrorHandler` | `echox.DefaultErrorHandler` |
| `ginx.WithServerAddr`, `echox.WithServerAddr` | `WithAddr` (absorbed the lazy `*http.Server` creation) |
| `ginx.QueryBinding` | unexported |

`WithErrorHandler` used to mean the native handler shape on ginx/hertzx and the
`httpx.ErrorHandler` shape on the other three, so portable setup code could not
be written. It is now `httpx.ErrorHandler` everywhere. `DefaultErrorHandler` is
the name on all five and means *this adapter's default in its native shape* —
the five signatures differ on purpose, and for stdx the native shape happens to
be `httpx.ErrorHandler`. It is not interchangeable across adapters;
`WithErrorHandler` is the portable surface.

Filled in for parity: `UseNative` on echox/fiberx/hertzx (ginx already had it),
and `FromEcho`/`FromFiber`/`FromStd` alongside `FromGin`/`FromHertz`. **stdx has
no `UseNative` on purpose**: it has no framework chain to hand a layer to, so
the only possible body would be `Use(AdaptStdMiddleware(mw))`.

**Binding no longer validates**

Adapters used to run go-playground/validator with gin's `binding` tag after
every successful decode. That made multi-source binding impossible: with

```go
struct {
    Name string `json:"name"`
    ID   string `uri:"id" binding:"required"`
}
```

the sequence `BindJSON` → `BindHeader` → `BindQuery` → `BindURI` — which is what
`protoc-gen-sphere` generates — failed with 400 at `BindJSON`, validating a
`uri` field nothing had populated yet. All five adapters did this.

`Bind*` now decodes and does not validate; a `binding` tag means nothing to
httpx. Validation belongs above this layer: `protoc-gen-sphere` emits
`protovalidate.Validate(&in)`, and code that wants something else calls its own
validator. `WrapBindError` is unchanged — a *decode* failure is still 400.

`binding.Validator` is never assigned. Mutating a process-wide variable in a
third-party package would disable validation for unrelated gin code in the same
binary, so ginx instead discards the validator's verdict; gin's multipart and
JSON decoders are not reproducible outside its `binding` package.

**Removed**

| Removed | Instead |
| --- | --- |
| `httpx.WithJson`, `httpx.H` | `sphere/server/httpz.WithJson` — honors a handler-set status, routes 204/304 through `NoContent`, returns `DataResponse[T]` |
| `httpx.AsResponseInfo` | `ResponseInfo` is part of `Context`; read `ctx.StatusCode()` |
| `httpx.ListenAndAutoShutdown` | no callers existed; `httpx.Start`/`Close` remain |

**`httpx.ParseError` no longer falls back to `err.Error()`**

An error carrying no `MessageError` now yields an empty message instead of its
raw text. `ParseError` is the default `ErrorParser` in `sphere/httpz`, so that
string reached a response body — driver errors, SQL, panic text. Anything that
renders should use `RenderError`/`ClassifyError`, which still substitute
`http.StatusText(status)` and are unchanged. `ParseError(nil)` returns
`(0, 500, "")` instead of panicking.

**Anonymous wildcards are rejected at registration**

`/files/*` used to panic on ginx/hertzx with a framework-worded message and be
accepted by echox/fiberx/stdx, while `httpx.ValidateWildcardPath` called it
legal. All five now reject it identically with httpx's own error. Write
`/files/*name`. A named wildcard registers directly on every adapter, so
`httpx.FixWildcardPathIfNeed` is not something application code should call —
it is adapter-internal in practice and its result must not be passed to
`Handle`.

### Bug fixes

- **404 and 405** bypassed the configured `ErrorHandler` on ginx/hertzx, which
  answered with the framework's plain-text body, and reported a wrong-method
  request as 404 because `HandleMethodNotAllowed` defaults off. All five now
  render both through `httpx.ErrorHandler` with the same status, body and
  Content-Type.
- **`BindURI` disagreed with `Param` for named wildcards**, three ways and
  silently: ginx added a leading slash, echox and fiberx bound `""` with no
  error, hertzx and stdx were correct. Generated wildcard routes therefore bound
  nothing on two adapters. The contract is now that `BindURI` into `uri:"x"`
  yields exactly `Param("x")`.
- **The named-wildcard mapping was process-global** on echox/fiberx, keyed by
  the *normalized* pattern, so two engines in one process that normalized to the
  same key overwrote each other. `FullPath()` reported the other engine's
  pattern and `Param` returned `""`. It is per engine now. This mattered because
  `FullPath` feeds `httpz.MatchOperation`, which gates auth and rate limiting.
- **`FullPath()` leaked httpx's own wildcard rewrite** on echox/fiberx,
  reporting `/files/*` for a route registered as `/files/*filepath`.
- **echox did not percent-decode route params**, so `/files/a/b%2Fc.txt` bound
  `a/b%2Fc.txt` where the other four bound `a/b/c.txt`.
- **fiberx `Header("Host")` and `BindHeader`** returned the Host where the other
  four return empty, and after the first fix the two disagreed with each other.
- **`Engine.Stop` never force-closed** when the caller's context expired, leaving
  connections serving with no way to cut them. ginx/echox/stdx now force-close
  via `http.Server.Close`. fiberx also used to report a failed shutdown as
  success whenever the caller's context happened to be done.
- **A `ginx` binding failure could be reported as success.** Any
  `*reflect.ValueError{Kind: Invalid}` panic was dropped and `nil` returned; a
  panic of that shape from the *decode* phase left a half-written destination
  and no error. It is now dropped only when the panicking stack shows gin's
  validator below it.
- **An `ErrorHandler` that renders nothing** left echox/fiberx/stdx answering
  200 with an empty body — including for a path that matched no route. The
  error's own status is now the floor on all five.
- **`stdx.FromStd`**'s `Native.Engine()` is `nil` and any method call on it
  panicked, undocumented. Documented now, deliberately still `nil`: a stand-in
  engine would answer for a route table and a trusted-proxy policy that do not
  exist.
- **echox's `New`** overwrote a caller's `echo.HTTPErrorHandler` unconditionally
  while `NewConfig` kept a guard to preserve it. The precedence is one
  documented rule now.

### Known behavior notes

- **404/405 body is not uniform when a custom `ErrorHandler` writes nothing.**
  Status is (404/405 on all five), and the default handler's body is identical
  everywhere. But a handler that only logs leaves ginx and hertzx with their
  framework text and the other three empty. Suppressing that text would mean the
  adapter forging a commit for a handler that declined to write, so it is left
  alone.
- **fiberx cannot cut an established connection on `Stop`**, and neither can
  hertzx — fasthttp has no `Server.Close` and hertz's is `Shutdown` with an
  expired context. Both close the listener. Declared as
  `httpxtest.Caps.ForcedStopCutsConnections` and verified in both directions
  against a real listener, so the claim cannot drift into documentation.
- **`FromEcho` cannot resolve a named wildcard**, even on a route this adapter
  registered: echo's group middleware slots are shared across routes, so only the
  per-engine table resolves it and a detached context has no pointer to one.
  `FullPath` reports echo's `/files/*`, `Param("name")` is empty, and `Param("*")`
  reaches the value. `FromFiber` does resolve it, because there the mapping is on
  the request.
- **fiberx pays one extra allocation per request on routes with a named
  wildcard.** Nothing elsewhere; pinned by `fiberx/wildcard_alloc_test.go`.
- **hertzx's 405 has no `Allow` header** while ginx's does — hertz does not
  expose the matched methods.
- `HandleMethodNotAllowed` being on means `HEAD` against a GET-only route is now
  405 rather than 404, uniformly.

### Downstream migration

`sphere` currently pins `httpx v0.0.4` but already uses `httpx.AsMiddleware` and
`httpx.Interceptor`, which v0.0.4 does not contain, so `go build ./...` fails
there today — `sphere` is blocked on this release rather than merely benefiting
from it. Line numbers below are against the state at the time of writing.

| Where | Change |
| --- | --- |
| `sphere/storage/fileserver/fileserver.go:233` | `int(result.Size)` → `result.Size` (`DownloadResult.Size` is already `int64`) |
| `sphere/storage/test/fileserver_http_test.go:544` | `size int` → `size int64` in the `miniContext` responder, and drop the now-redundant `int64(size)` at the `io.CopyN` below |
| `sphere-layout/internal/pkg/httpsrv/httpsrv.go:45` | `ginx.WithHTTPXErrorHandler(...)` → `ginx.WithErrorHandler(...)` |
| `sphere/server/httpz/sse_test.go:505` | `ginx.WithServerAddr(addr)` → `ginx.WithAddr(addr)` |
| `sphere-layout/internal/pkg/httpsrv/httpsrv_test.go:21,27` | `httpx.WithJson` → `httpz.WithJson` (byte-identical response) |

Also worth doing, not required to compile:

- `sphere/storage/fileserver/fileserver.go` `RegisterFileDownloader` can register
  `/*filename` directly and read `Param("filename")`. Calling
  `httpx.FixWildcardPathIfNeed` and registering its result is now wrong as well
  as redundant — the result is the anonymous form, which this release rejects.
  (Registering the named form directly also works on v0.0.4, so this can land
  before the upgrade.)
- `sphere/server/httpz/error.go:95` — the `|| message == err.Error()` half of
  that test is dead now that `ParseError` no longer leaks, and it was the half
  that discarded a custom parser's message when it happened to equal
  `err.Error()`. The comment block above it describes behavior that no longer
  exists.
- `sphere/server/httpz/metadata.go` `EndpointsToMatches` indexes every route
  twice, verbatim and in anonymous form, to absorb the old `FullPath()` leak.
  That is no longer necessary.
- Three `httpx.ResponseInfo` mentions in `sphere` comments describe an
  `AsResponseInfo`-era "when available" probe and are stale prose.
- Nothing in `protoc-gen-sphere` changes: generated code uses only
  `httpx.Context`/`Handler`/`Router`, `Group`/`Handle`, `Bind*` and
  `ctx.Context()`, and there are no `binding` struct tags anywhere in the sphere
  stack, so dropping validation is a no-op for it.

### Releasing

The root module and the adapters cannot be tagged together: the adapters'
`go.mod` must require a root version that is already downloadable, and
`scripts/check-release.sh` enforces it.

```
git tag v0.0.5 && git push origin v0.0.5     # root first
make prepare-release TAG=v0.0.5              # bump adapter go.mod/go.sum
# commit those, then
make release-check TAG=v0.0.5                # per-adapter tidy + test with GOWORK=off
make tag-all TAG=v0.0.5                      # ginx fiberx echox hertzx stdx
```

`stdx` has never been tagged, so `stdx/v0.0.5` is its first release. It is also
absent from `scripts/check-api-compat.sh`, whose baseline is still `v0.0.3`;
both want updating after this release. CI runs `make check` only, so neither
affects it.
