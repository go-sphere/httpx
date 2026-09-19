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
and `FromEcho`/`FromFiber`/`FromStd` alongside `FromGin`/`FromHertz`. `UseNative`
is now the only way to mount a native middleware — see *One middleware type*
below. **stdx has no `UseNative` on purpose**: it has no framework chain to hand
a layer to, so the only possible body would be `Use(AdaptStdMiddleware(mw))`.

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

**One middleware type, and it is the composed one**

v0.0.4 had two ways to write a layer. `httpx.Middleware` was
`func(Context) error`, continued the chain with `ctx.Next()`, and took its own
handler slot in the framework's chain. `httpx.Interceptor` was
`func(next Handler) Handler`, was composed into the route at registration, and
held no per-request state.

v0.0.5 ships **one**. The composed form takes the `Middleware` name; the
Next-driven form is gone, along with `Context.Next()` and the machinery that
converted between the two.

```go
type Middleware func(next Handler) Handler   // was Interceptor
Use(m ...Middleware)                         // was UseInterceptor
Group(prefix string, m ...Middleware) Router
```

| v0.0.4 | v0.0.5 |
| --- | --- |
| `httpx.Interceptor` | `httpx.Middleware` |
| `httpx.InterceptorScope` | `httpx.MiddlewareScope` |
| `scope.UseInterceptor(...)` | `scope.Use(...)` |
| `httpx.ComposeInterceptors` | `httpx.ComposeMiddleware` |
| `httpx.InterceptorChain`, `httpx.NewInterceptorChain` | `httpx.MiddlewareChain`, `httpx.NewMiddlewareChain` |
| `httpx.InterceptorFallback`, `httpx.NewInterceptorFallback` | `httpx.MiddlewareFallback`, `httpx.NewMiddlewareFallback` |
| `httpx.Middleware` (`func(Context) error`) | *removed* |
| `httpx.UseInterceptor(scope, ...)` (free function) | *removed* — `Use` is a method on every scope |
| `httpx.AsMiddleware`, `httpx.AsInterceptor` | *removed* — there is nothing to convert |
| `Context.Next()` | *removed* — a layer is handed `next` and calls it |
| `ginx.AdaptGinMiddleware`, `echox.AdaptEchoMiddleware`, `fiberx.AdaptFiberMiddleware`, `hertzx.AdaptHertzMiddleware` | *removed* — use `UseNative` |

**Migrating a `func(httpx.Context) error` layer.** Wrap the body in a closure
that takes the rest of the chain, and replace `ctx.Next()` with `next(ctx)`:

```go
// v0.0.4
func RequestID(ctx httpx.Context) error {
    ctx.SetContext(withRequestID(ctx.Context()))
    return ctx.Next()
}

// v0.0.5
func RequestID(next httpx.Handler) httpx.Handler {
    return func(ctx httpx.Context) error {
        ctx.SetContext(withRequestID(ctx.Context()))
        return next(ctx)
    }
}
```

A layer that stopped the chain by returning without calling `Next` now returns
without calling `next` — same shape, one less thing to know about:

```go
// both releases
func RequireAuth(next httpx.Handler) httpx.Handler {
    return func(ctx httpx.Context) error {
        if !authorized(ctx) {
            return ctx.Text(http.StatusUnauthorized, "denied")
        }
        return next(ctx)
    }
}
```

The two signatures are unrelated — `func(Context) error` is not assignable to
`func(Handler) Handler`, and no untyped func literal fits both — so every call
site that passed the old form is a **compile error**, never a silent change of
behavior. The one shape that *would* have changed silently, `Group(prefix, mw)`,
is covered by the same rule.

**Three behavior changes come with the collapse**, because the composed form's
rules are now the only rules:

- A layer registered with `Use` runs **inside** anything registered with
  `UseNative` on the same scope, whatever the call order. On v0.0.4 a
  `Middleware` interleaved with native middleware in registration order.
- An error from an inner layer is rendered **at the route**, where the chain was
  composed, not at the layer that produced it. A layer that logs or measures the
  outcome must use the error `next` returned, not only `Context.StatusCode()`.
- `engine.Use` after a `Group` exists now reaches the routes that group registers
  afterwards, on all five adapters. On v0.0.4 that worked on echox and fiberx
  (which composed `Use` into the route) and silently reached nothing on
  ginx/hertzx/stdx.

**Can the new form still do what the old one did?**

Yes, with exactly one exception. Every row below was measured on all five
adapters plus the caller-built fiber suite, not reasoned about:

| What a v0.0.4 `Middleware` could do | v0.0.5 | How it reads now |
| --- | --- | --- |
| run before the rest of the chain | yes | before `next(ctx)` |
| run after it | yes | after `next(ctx)` |
| stop the chain | yes | return without calling `next` — no `Abort` bookkeeping, no second-`Next` hazard |
| inspect the error the chain returned | yes | `err := next(ctx)`; it propagates outward through every layer |
| recover a downstream panic and answer instead | yes | `defer`/`recover` around `next(ctx)` |
| cover unmatched paths (404/405) | yes, at engine scope | `engine.Use` runs for a request no route handled; a group's layers do not, because a 404 belongs to no group |
| apply only to routes registered after it | yes | the chain is snapshotted per route at registration, as gin's `Use` always was |
| replace the request `context.Context`, short-circuit with a response | yes | unchanged — same `Context` |
| **sit outside a native framework middleware** | **no** | native is always outermost on its scope |

The exception is real and cannot be worked around by restructuring. A v0.0.4
`Middleware` occupied its own native handler slot, so `Use(A)`,
`UseNative(N)`, `Use(C)` produced A → N → C. Now every httpx layer on a scope
shares the route's single slot, which is what makes the chain cost one call per
layer and allocate nothing — and that slot is inside the native ones. Moving the
httpx layer to a parent group does not help; measured, with `OUTER` on the parent
and the native layer on the child:

```
native-pre  OUTER-pre  INNER-pre  handler  INNER-post  OUTER-post  native-post
```

Who this bites: code that wanted its own layer to wrap a *native* recovery or
logging middleware — with native outermost, a native `gin.Recovery()` absorbs the
panic before an httpx layer above it can see it. The way out is to recover in the
httpx form instead (`sphere/server/middleware/logger.RecoveryLog`), which is what
`sphere-layout` already does. Native middleware registered directly on the
framework engine before it reaches `WithEngine` is unaffected — it was outermost
in v0.0.4 too.

**Native middleware is `UseNative` only**

The four `Adapt<Framework>Middleware` functions are removed and cannot be
expressed in the composed form. `AdaptGinMiddleware`'s own doc said why: *"The
native middleware may also continue the chain itself with gin's Next, which works
because every httpx middleware occupies its own gin handler slot."* It no longer
does — all of a scope's httpx layers share the route's single slot — so a native
middleware's `c.Next()` would advance gin past that slot instead of into the
layer below. The same reasoning holds for hertz (`ctx.Next`), fiber (`c.Next`)
and echo (the `echo.HandlerFunc` a `MiddlewareFunc` is handed is the whole
composed chain, not the next layer).

```go
router.Use(ginx.AdaptGinMiddleware(gin.Recovery()))   // v0.0.4
router.(*ginx.Router).UseNative(gin.Recovery())       // v0.0.5
```

`UseNative` was already the documented preference on all four. `stdx` still has
none, for the reason it never had one.

**`AdaptStdMiddleware` stays**, on all five, rewritten in the composed form.
`func(http.Handler) http.Handler` is framework-neutral, so it maps onto
`func(next Handler) Handler` more directly than onto the Next-driven form. It
returns `httpx.Middleware` as before, so `router.Use(x.AdaptStdMiddleware(mw))`
is unchanged at the call site. Everything the implementations bought is
preserved: request mutation, writer wrapping, short-circuiting, and on `stdx`
the isolation of a timed-out handler's continuation (`http.TimeoutHandler`) and
the writer/header-cache behavior.

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

### What made one form possible

Collapsing the two forms only works if the survivor can do everything the other
one could. Two things had to become true first; both are in this release.

**An engine-scope chain covers unmatched paths**

Measured on v0.0.4, all five adapters, with the two forms side by side:

```
request /known  →  Use=1  Interceptor=1
request /nope   →  Use=1  Interceptor=0
```

A composed chain goes into registered routes, and a 404 never reaches a route, so
nothing composed into one ran. That is why an access log, a panic recovery layer
and a CORS preflight had to be written in the Next-driven form — all three must
see a request that did not route — and it was the last thing only that form could
do. Both columns are `1` on all five now, which is what left one form to keep.

The rule is a split, not a blanket:

| | matched route | unmatched path (404/405) |
| --- | --- | --- |
| engine-scope `Use` | runs | **runs** |
| group-scope `Use` | runs | never runs |

A group's chain must not cover a 404: the request belongs to no group, so there is
no scope to pick, and picking one by prefix would make the answer depend on how
the route table happens to be split up. Both halves are tested, on all five
adapters plus the caller-built-`fiber.App` suite, by `httpxtest`'s
`MiddlewareScope` group.

An engine-scope layer may also *answer* an unmatched path — return without
calling `next` and the fallback does not render — which is what a CORS preflight
or a single-page-app rewrite needs. A layer that returns without answering does
not turn a 404 into a 200 either: the fallback's own status is the floor.

The composition is cached against the chain's current layer list and rebuilt only
after a `Use` call, because the fallback is installed inside `New` (before any
registration, so it cannot snapshot) and composing per request would allocate one
closure per layer on the cheapest request there is to send. A 404 on ginx costs
7 allocations with no middleware and 7 with eight — flat; pinned by
`ginx/middleware_alloc_test.go`. The assumption is that registration finishes
before serving starts; a `Use` racing a request is race-free rather than merely
unlikely (the layer list is replaced, never mutated in place) and that request
sees either the old chain or the new one.

v0.0.4's `Caps.InterceptsUnmatchedPathInsideUse` recorded where the composed
chain sat relative to engine-scope `Use` middleware on an unmatched path (inside
on ginx/hertzx/stdx, after on echox/fiberx). The capability is gone with the
distinction: `Use` *is* the composed chain now, so there is nothing for it to sit
inside or after.

**Late registration reaches groups that already exist**

Measured on v0.0.4, all five:

```go
e.UseInterceptor(A); g := e.Group("/"); e.UseInterceptor(B); g.GET("/x", h)
// A runs, B does not, silently
```

`Group` copied its parent's slice, so a layer registered afterwards reached
nothing. A group now holds a *reference* to its parent's chain and a route's chain
is resolved when the route is registered, which is what the contract already said.
Routes registered before the call still keep the chain they were registered with —
that is gin's rule for its own `Use`, and it is what makes a registered route
immutable. The reference costs one pointer hop per route registration and nothing
per request: the composed chain is still built once, still holds no per-request
state, still 0 allocations.

The Next-driven `Middleware` had the same trap, and per-framework answers to it:

| | `ginx` | `hertzx` | `stdx` | `echox` | `fiberx` |
| --- | --- | --- | --- | --- | --- |
| late `Engine.Use` reached an existing group, v0.0.4 | no | no | no | yes | yes |
| late `Engine.UseInterceptor`, v0.0.4 | no | no | no | no | no |
| `Engine.Use`, v0.0.5 | **yes** | **yes** | **yes** | **yes** | **yes** |

That row was documented as a deliberate asymmetry in v0.0.4 because unifying it
would have meant reaching into three frameworks' group semantics. With one form
the chain is entirely ours and the question does not arise.

**What it costs and what it buys**

A bare 10-layer chain, measured through each framework's own dispatcher
(`BenchmarkHTTPXTestSuite/.../Middleware10`):

| | `ginx` | `echox` | `fiberx` | `hertzx` | `stdx` |
| --- | --- | --- | --- | --- | --- |
| ns/op | 47 | 66 | 93 | 304 | 35 |
| allocs/op | 0 | 2 | 1 | 4 | 0 |

Every allocation left is the adapter's per-request cost on an empty route, not
the chain: the same scenario with no middleware measures 34 / 57 / 81 / 296 / 27
ns at identical allocation counts. On v0.0.4 the Next-driven form allocated one
layer object per layer per request; a 10-layer chain cost 438–637 ns everywhere
except stdx.

**The contract wording changed**

The composed form's documented coverage was "composed into registered routes, not
unmatched paths". It is now the split above, restated in `middleware.go`,
`CLAUDE.md` and `README.md`. `interceptor.go` was renamed to `middleware.go` with
it; the root `router.go` keeps `Handler`, `Registrar`, `Router` and `Engine`, and
`Middleware`/`MiddlewareScope` moved out of it into `middleware.go` next to the
chain types.

Two adapter-facing types are new in the root module, `httpx.MiddlewareChain` and
`httpx.MiddlewareFallback`. Application code does not need them; they exist so
the two rules above are written once instead of five times, and so a third-party
adapter can get them right the same way the official five do.

### Bug fixes

- **echox 404'd every route under a group prefix ending in `/`.** echo joins a
  group prefix and a route path by string concatenation, so `Group("/")` followed
  by `GET("/x")` registered `//x` and nothing matched `/x`; `Group("/api/")` +
  `GET("")` registered `/api` where the other four register `/api/`. `BasePath()`
  reported the right thing either way, so it was silent, and the shared suite only
  ever called `Group("")` — the one prefix shape that cannot expose it. It was
  fatal in practice rather than at the edges: every `protoc-gen-sphere` service
  registers on `route.Group("/")`, and `sphere-layout` uses `engine.Group("/")`
  and `api.Group("/")` throughout, so on echox every generated route 404'd.
  Pre-existing since the adapter was written, not a regression. echox now derives
  what echo is handed from the same `joinPaths`-maintained base path the other
  four use, and `Static`/`StaticFS`/`HandleStd` — which register through `Handle`
  — are fixed with it. The suite gained a `GroupPrefix` group covering `""`,
  `"/"`, `"/api"`, `"/api/"`, nested combinations, an empty or `"/"` route path on
  each, and each mount kind on each, for all five adapters plus the
  caller-built-`fiber.App` suite.
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
- **`SetContext` below is visible above on all five now.** Every layer of a
  composed chain is handed the same `Context`, so a `SetContext` in the handler
  is what an outer layer reads after `next` returns. hertzx used to diverge —
  the Next-driven form gave each layer its own context with its own `baseCtx`,
  so an outer layer kept the value it had set itself. Pinned by
  `conformance/TestWrapperSetContextAfterNext`.

### Downstream migration

Line numbers below are against the state at the time of writing.

The largest piece is the middleware collapse: `sphere/server/middleware` ships
every layer twice, once as a Next-driven `httpx.Middleware` wrapper and once as
an `httpx.Interceptor`. Each pair becomes one function under the plain name —
delete the wrapper, rename the `*Interceptor` half:

| Wrapper to delete | Rename to it |
| --- | --- |
| `logger/logger.go:29` `Log` | `logger/logger.go:35` `LogInterceptor` |
| `logger/logger.go:83` `RecoveryLog` | `logger/logger.go:88` `RecoveryLogInterceptor` |
| `ratelimiter/rate_limiter.go:76` `NewRateLimiter` | `ratelimiter/rate_limiter.go:83` `NewRateLimiterInterceptor` |
| `ratelimiter/rate_limiter.go:148` `NewRateLimiterByClientIP` | `ratelimiter/rate_limiter.go:155` `NewRateLimiterByClientIPInterceptor` |
| `auth/auth.go:162` `NewAuthMiddleware` | `auth/auth.go:169` `NewAuthInterceptor` |
| `auth/permission.go:20` `NewPermissionMiddleware` | `auth/permission.go:25` `NewPermissionInterceptor` |
| `cors/cors.go:83` `NewCORS` | `cors/cors.go:94` `NewCORSInterceptor` |
| `online/online.go:83` `(*Online).Middleware` | `online/online.go:89` `(*Online).Interceptor` |

All eight wrappers are one line — `httpx.AsMiddleware(<the other one>(...))` —
so deleting them is mechanical. The ninth pair is not:
`selector/selector.go:83` `NewSelectorMiddleware` has a body of its own that
calls `ctx.Next()` (`selector.go:90`) and must be deleted outright, with
`selector.go:99` `NewSelectorInterceptor` renamed to `NewSelectorMiddleware`.

`sphere/server/middleware/chain_bench_test.go` exists to compare the two forms
(see its package comment at lines 2-3, and the `httpx.UseInterceptor` calls at
83, 87 and 89). With one form it measures one thing; either drop the `mode`
dimension or delete it.

`sphere-layout` has three `engine.Use(...)` call sites, all in
`internal/pkg/httpsrv/httpsrv.go` (lines 42, 48 and 64) — they keep working
unchanged once the constructors above return the composed type, and their
comments about why engine-wide concerns "stay on `Use`" rather than becoming
interceptors are now describing a distinction that no longer exists.
`internal/server/dash/web.go` drops the second line at each
`httpx.UseInterceptor(...)` call (72, 81, 84 and 99) in favour of `Use` or
`Group`'s variadic.

The two `sphere` test fakes that implement `httpx.Router`/`httpx.Engine` by hand
need **no** change after all: `stubEngine`/`stubRouter`
(`sphere/server/service/file/web_test.go:83,85,103,109`) and `miniRouter`
(`sphere/storage/test/fileserver_http_test.go:264,271`) already spell their
methods `Use(...httpx.Middleware)` and `Group(string, ...httpx.Middleware)`, and
those are the v0.0.5 signatures. Only the meaning of the type changed, and their
bodies are no-ops.

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
  stack, so dropping validation is a no-op for it. Its `route.Group("/")` takes no
  extra arguments, so the `Group` signature change is a no-op too — and the
  echox prefix fix means those routes answer on echox for the first time.
- `sphere/server/middleware/selector/selector.go`'s package comment (line 2) and
  the doc on the surviving constructor still describe `ctx.Next()`.
- `sphere/server/middleware/chain_bench_test.go`'s package comment describes a
  comparison that no longer has two sides.

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
