# Middleware chain fusion: measurements and feasibility

2026-09-18, Apple M1 Pro, Go 1.27.1, GOMAXPROCS=4, same machine as
[GIN_DEPTH_REPORT.md](GIN_DEPTH_REPORT.md). This round first used prototype benchmarks to answer
"how much is there to gain by adapting a whole run of middleware once instead of one layer at a
time, and what semantics does that break", and then implemented the approach in ginx.

**Read this first.** The fusion approach recorded here (sections 2–6) was implemented, measured,
and then **removed**: it was dominated by what was then called `httpx.Interceptor`, and it
carried one correctness risk that could not be designed away. The conclusion and the deletion
list are in [section 9](#9-fusion-was-removed-why). Section 1 on the deep-chain cliff and
sections 7–8 on the middleware signature are still valid, and they are what the deletion
decision rests on. The earlier sections are kept because the data is the evidence, not because
the approach survived.

> Two renames happened after this was written. `httpx.Interceptor`
> (`func(next Handler) Handler`) is what v0.0.5 renamed to `httpx.Middleware` — it is the one
> and only middleware form today. What this report calls "Middleware" or "per-layer adaptation"
> is the Next-driven `func(Context) error` form, which no longer exists. Read every
> "Interceptor" below as "today's `Middleware`", and every "Middleware" as "the removed
> Next-driven form".

## 1. Re-explaining the deep-chain cliff

The previous round attributed 430 ns at 10 layers to "per-layer allocation compounding with
recursive forwarding". The added depth curves show that **the cliff is not introduced by the
compatibility layer at all — it is a fixed consequence of call nesting depth, and native gin
hits it too**, just at a greater depth:

| Implementation | Frames per layer | ns/layer before the cliff | Cliff at |
| --- | ---: | ---: | ---: |
| Pure call chain (no framework, interface Next) | 2 | ≈2.5 | 32 |
| Native gin | 2 | ≈4.3 | 24 |
| Fused httpx (prototype) | 2 + entry | ≈2.9 | 20 |
| Per-layer adaptation (before the change) | 4 | ≈20 | 9–10 |

Gin depth curve (`layers=8…24`, n=6, see
`fusion-depth-gin-stats.txt`):

| Layers | native ns/op | fused prototype ns/op |
| ---: | ---: | ---: |
| 8 | 55.52 | 82.89 |
| 12 | 72.18 | 103.60 |
| 16 | 89.46 | 107.05 |
| 18 | 98.27 | 114.05 |
| 20 | 107.1 | **379.1** |
| 24 | **390.1** | 392.6 |

Two things ruled out:

- **Not Go stack growth.** Growing the goroutine stack with 4096 levels of recursion before the
  timed section still gave 382.1 ns for 20 fused layers
  (`fusion-depth-grown-stack.txt`).
- **Not GC.** With `GOGC=off`, 20 fused layers measured 383–392 ns — no material difference from
  the default build.

A frame-size experiment (0 / 256 / 1024 bytes of locals per layer, frame *count* unchanged,
`fusion-frame-size.txt`) shows bigger frames move the cliff
earlier: pad=0 jumps between 24 and 32 layers, pad=256 between 20 and 24. **No hardware
performance counters were collected, so the cliff cannot be attributed to a specific
return-address predictor, cache, or hardware threshold**; all that is established is that the
number *and* size of nested frames together determine where it lands.

Hence the core judgement of this round: per-layer adaptation walks four frames per layer
(`Gin.Next → adapter closure → user middleware → httpx.Next`) and allocates an object, so it hits
at 9–10 layers the wall native code only meets at 24. **The direction is to reduce frames and
frame size per layer, not to remove that 16-byte allocation on its own.**

## 2. Prototype results

The prototype composes consecutive httpx middleware into one chain; the framework sees a single
middleware, `Next` walks the chain, and each layer's state lives in `Next`'s stack frame. Two
shapes:

- `fused`: composed **above** the adapter using `httpx.Middleware` (one extra wrapper object,
  2 allocations).
- `fusedInAdapter`: implemented **inside** the gin adapter (reuses the entry object, 1
  allocation). Only gin implemented this one.

Four frameworks x 4 depths x 3–4 modes, 6 samples of 300ms each, all differences p=0.002
(`fusion-prototype-stats.txt`):

| Framework | Layers | native | httpx then | fused | fused in adapter | allocs then | fused allocs |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| gin | 1 | 33.17 | 48.86 | 78.02 | 64.70 | 1 | 2 / 1 |
| gin | 5 | 44.78 | 129.00 | 87.73 | 74.55 | 5 | 2 / 1 |
| gin | 10 | 63.83 | 433.55 | 102.10 | 88.23 | 10 | 2 / 1 |
| gin | 20 | 107.0 | 616.7 | 392.7 | 379.3 | 20 | 2 / 1 |
| echo | 1 | 45.23 | 86.47 | 120.15 | — | 3 | 4 |
| echo | 5 | 92.92 | 246.80 | 128.00 | — | 11 | 4 |
| echo | 10 | 159.7 | 446.5 | **145.7** | — | 21 | 4 |
| echo | 20 | 282.9 | 1038.0 | 419.4 | — | 41 | 4 |
| fiber | 1 | 66.27 | 65.27 | 93.78 | — | 0 | 1 |
| fiber | 5 | 75.59 | 90.26 | 104.10 | — | 0 | 1 |
| fiber | 10 | 87.49 | 359.90 | 119.45 | — | 0 | 1 |
| fiber | 20 | 110.3 | 634.8 | 406.1 | — | 0 | 1 |
| hertz | 1 | 120.3 | 158.4 | 188.3 | — | 3 | 4 |
| hertz | 5 | 135.7 | 257.2 | 196.1 | — | 7 | 4 |
| hertz | 10 | 157.8 | 579.5 | 212.4 | — | 12 | 4 |
| hertz | 20 | 437.0 | 817.8 | 500.6 | — | 22 | 4 |

How to read it:

- Allocation count goes from linear in the number of layers to constant. Gin at 10 layers: 10 → 1.
- At 10 layers: gin 434→88 (-80%), fiber 360→119 (-67%), hertz 580→212 (-63%), echo 447→146
  (-67%). Fused echo at 10 layers beats *native* echo, because native echo also allocates once
  per layer.
- **Fusion is slower at 1 layer** (gin 48.9→64.7): the extra entry object and one more level of
  forwarding are not amortized. **Do not fuse when there are very few layers.** On this data the
  gin crossover is between 2 and 3 layers.
- 20 layers is still past the cliff. Fusion pushes the wall from 9–10 layers out to 20; it does
  not remove it. Fiber and echo jump at 20 as well.

## 3. The real shape: nested groups

The sphere-layout registration shape is "2 engine-level layers (access log, recovery) + 1 layer
per nested group (auth, permission)" — 4 layers spread over 3 scopes. In that shape (n=6, 400ms,
`fusion-realistic-scopes-stats.txt`):

| Mode | ns/op | allocs/op | vs native |
| --- | ---: | ---: | ---: |
| native gin | 42.59 | 0 | — |
| httpx then | 97.12 | 4 | +128% |
| fusing within a single `Use` call only | 112.90 | 4 | +165% |
| fusing across scopes | 70.88 | 1 | +66% |

There is a design-deciding result here: **fusing only within one `Use` call is slower than doing
nothing** (97→113 ns). In the real shape most scopes register exactly one middleware, so fusing
two layers does not repay the wrapper cost, and not a single group-to-group round trip is saved.
The gain comes only from **composing the whole path across `Group` inheritance**, where 4
allocations drop to 1 and the overhead falls from +54 ns to +28 ns.

The absolute scale has to be stated plainly: in the 4-layer shape fusion saves about 26
ns/request. A JSON1K response on the same machine costs about 1250 ns, and a real endpoint adds
a database or RPC on top. **The value of this optimization is removing allocation that grows
with depth and pushing the cliff back to roughly where native sits — not end-to-end throughput.**

Recycling the remaining allocation with `sync.Pool` would save about another 11 ns (70.66→59.92
ns, `fusion-pooled.txt`). Not recommended: `Context` is contracted
to stay valid for the whole request, and pooling would let a middleware that retained one read a
recycled object. (That is also why an earlier round removed pooling; the reasoning now lives on
the `Context` contract in `CLAUDE.md`.)

## 4. Semantic validation: what passed, what broke

So that feasibility was not assessed by guesswork, the prototype stage actually implemented a
fused version in ginx (`ginx/chain.go` plus registration bookkeeping in `Router`/`Engine`), ran
conformance against it, and then reverted it completely, keeping only the benchmarks and one
ordering test. The shipped implementation is in [section 6](#6-the-version-that-shipped).

Implementation points that exist to preserve semantics, not merely to string layers together:

- Per-layer error watermark: each layer reads `len(gc.Errors)` before and after calling the next
  one, preserving "an error recorded by a native middleware via `gc.Error` is visible to an outer
  layer after `Next`". Two integer comparisons, no allocation.
- Render at the failing layer: when a layer returns an error, run `gc.Error`, then (if nothing is
  committed and not already aborted) the error handler, then `Abort`, and propagate the error up
  as a return value. Rendering centrally at the entry instead would make middleware like
  `sphere/server/middleware/logger`, which reads `ctx.StatusCode()` after `Next`, log 200 instead
  of 401/500.
- Not calling `Next` counts as abort: if the layer index did not advance, `Abort`, with a
  sentinel to stop an aborted chain being re-entered by a second `Next`.
- Segments may only extend at the tail: replace in place only while this segment is still the
  last element of `group.Handlers`; if another handler was inserted meanwhile, start a new
  segment. Replacement rebuilds the closure over a cloned slice, so already-registered routes
  stay bound to the shorter chain they were registered with.
- After replacing in place on the engine's root group, gin's 404/405 chain has to be refreshed
  explicitly (one empty `engine.Use()` call).

Result: **the whole gin conformance suite passed** — error handlers, the std middleware bridge,
context wrapping, repeated `Use` calls, mixing in a native `gin.Recovery()` — with the only
failure being an unrelated file-descriptor check,
`TestAdversarialStaticHEADZeroFDLeaks/fiberx`.

But fusion does break one existing behaviour, and conformance did not cover it. The ordering test
added at the time (now `TestMiddlewareChainNativeBridgeOrder` in
`conformance/middleware_chain_conformance_test.go`) pins it: with a native middleware wrapped by
`ginx.AdaptGinMiddleware` sandwiched between two httpx middleware, the native one calls
`gc.Next()` internally — and after fusion, "next" on the gin chain is already the business
handler:

```text
expected: a-pre, native-pre, b-pre, handler, b-post, native-post, a-post
fused:    a-pre, native-pre, handler, native-post, b-pre, b-post, a-post
```

That test passes under per-layer adaptation and fails under the fused prototype.
`AdaptStdMiddleware` and `AdaptEchoMiddleware` are unaffected — they drive downstream through
`ctx.Next()`. `AdaptGinMiddleware`, `AdaptHertzMiddleware` and `AdaptFiberMiddleware` hand the
native context to native middleware and are all exposed.

The fix is to recognize such middleware at registration and give each one its own native handler
slot, segmenting instead of fusing. Recognition was done by comparing
`reflect.ValueOf(m).Pointer()` against a sentinel, and a standalone experiment "validated" it —
**that validation was wrong**, because the sentinel and the middleware under test were created in
the same package. `AdaptGinMiddleware` can be inlined, and once inlined each caller gets its own
closure symbol, so a cross-package comparison always fails. The shipped implementation switched
to a method value to make the code pointer stable; see [section 6](#6-the-version-that-shipped).

One inherent limitation of the recognition remains: it can only recognize middleware produced by
`AdaptGinMiddleware`. A user's own equivalent wrapper — `AsNativeContext` plus a `gc.Next()` call
— cannot be recognized, and the order will still be wrong. That is why an explicit native
registration entry point (`UseNative(...gin.HandlerFunc)`, which also saves an adaptation) and an
escape hatch to disable fusion are both required.

An implementation detail worth recording: when decorating `httpx.Context` you cannot embed
`httpx.Context` directly — the field name `Context` collides with the interface's own `Context()`
method. Declare `type chainBase httpx.Context` first and embed that.

## 5. Conclusions and priorities

1. **Cross-scope chain fusion** is the only structural change that materially reduces multi-layer
   middleware cost: allocation goes from O(layers) to O(1), gin at 10 layers -80%, the real
   4-layer shape -27%. It must take effect across `Group` inheritance; fusing within a single
   `Use` call makes things worse.
2. **Set a minimum fusion length** (≥2; the measured gin crossover is between 2 and 3 layers) and
   leave 1 layer as it is.
3. **The native middleware bridge must segment**, and an explicit native registration API is
   needed. Matching function signatures do not imply compatible behaviour; the ordering test
   should land in conformance first, as a regression gate.
4. **Keep the hot path's frames small**: error collection and error rendering belong in separate
   functions, and large locals must stay out of `Next`. The frame-size experiment shows this
   affects both per-layer cost and where the cliff lands.
5. **Do not pool** `Context` for that ~11 ns; it contradicts the request-lifetime contract.
6. **PGO as an aid only**, with the conclusion carried over from
   [GIN_DEPTH_REPORT.md](GIN_DEPTH_REPORT.md), and not as a default build setting for the library.
7. The cliff beyond 20 layers is **not** removed by fusion — native gin hits it at 24. Do not
   promise "close to native at any depth".

Not done: the fused implementation was validated semantically on gin only; echo, fiber and hertz
have prototype timings but no semantic work of their own. End-to-end network differences were not
measured. Registering routes after `Use`, and interleaving route registration with `Use`, rely on
existing conformance and were not specifically covered. The prototype has neither the per-layer
error watermark nor render-at-the-failing-layer, so the numbers in section 2 are a lower bound on
the implementation, not directly transferable results.

## 6. The version that shipped

The approach was merged into ginx following the priorities in section 5 (`ginx/chain.go` plus
registration bookkeeping in `Router`/`Engine`). echox, fiberx and hertzx still adapted one layer
at a time.

### Measured results

Same machine, same `BenchmarkAdapter` (four frameworks x seven scenarios x native/httpx, 6
samples of 200ms). The other three frameworks served as controls and did not move. Only gin is
shown; "before" comes from the same benchmark in the preceding round's report:

| Scenario | native | httpx before | httpx after | allocs before | allocs after |
| --- | ---: | ---: | ---: | ---: | ---: |
| Empty | 31.45 | 32.91 | 33.79 | 0 | 0 |
| Middleware1 | 33.94 | 49.14 | 49.92 | 1 | 1 |
| Middleware5 | 45.59 | 130.70 | 79.62 | 5 | 1 |
| Middleware10 | 64.09 | 466.75 | 96.14 | 10 | 1 |
| Middleware20 | 107.9 | 620.70 | 400.3 | 20 | 1 |
| JSON1K | 1296 | 1252.50 | 1314 | 1 | 1 |
| StateParallel | 146.0 | 147.95 | 152.0 | 3 | 3 |

10 layers -79%, 5 layers -39%, 20 layers -35%. One layer and the no-middleware path are unchanged
by design (a single middleware keeps the per-layer wrapper, which is smaller than a chain
object). JSON1K and StateParallel have no middleware and differ only within noise. Before and
after come from two different sampling runs, so the percentages indicate magnitude only.

Real nested-group shape (2 engine-level layers + 1 per group over two levels,
`BenchmarkNestedScopes`, 6 x 400ms):

| Mode | ns/op | allocs/op |
| --- | ---: | ---: |
| native gin | 41.92 | 0 |
| httpx (after) | 76.14 | 1 |

That is +34 ns / +82%, down from +54 ns / +128%, with 4 allocations becoming 1. Slightly above
the prototype's 70.88 ns in section 3; the difference is the per-layer error watermark and
render-at-the-failing-layer, which the prototype did not implement.

Depth curve (`BenchmarkGinDepthCurve`, 6 x 300ms): 8 layers 55.7 / 89.2, 18 layers 99.3 / 124.5
(native / httpx), about +3.5 ns per layer; the cliff is still at 20 layers (native 24), matching
the prototype.

### Final behaviour and API

- `Router.Use` / `Router.Group` accumulate consecutive httpx middleware into one segment,
  continuing to accumulate across group inheritance, registered as a single gin handler. A
  segment is extended only while its handler is still last in `group.Handlers`; extension rebuilds
  the closure over a cloned slice, so already-registered routes stay bound to the chain they were
  registered with. The engine root group refreshes gin's 404/405 chain after an in-place
  replacement.
- A single middleware keeps the per-layer wrapper, and is upgraded to a chain when another
  middleware joins the same position.
- `Router.UseNative(...gin.HandlerFunc)` / `Engine.UseNative`: register native gin middleware
  directly, with no adapter, ending the current segment. This is the recommended way to mix in
  native middleware.
- `AdaptGinMiddleware` now returns the method value `ginBridge{...}.run` rather than a function
  literal, so its code pointer is stable across all callers and registration can recognize it and
  give it its own slot. **The prototype's `reflect` comparison of function literals was wrong**:
  `AdaptGinMiddleware` can be inlined, and each caller then gets its own closure symbol, so the
  comparison always fails — which is exactly what the first cross-framework ordering test exposed
  during implementation.
- `ginx.WithoutMiddlewareFusion()`: fall back to one gin handler per layer. For hand-written
  native bridges (take the native context, call the framework's own `Next`) that cannot be
  recognized.
- New tests: `conformance/middleware_chain_conformance_test.go` (chain semantics shared by all
  four adapters: native bridge order, inner errors, aborting by not calling Next, group
  inheritance) and `ginx/chain_test.go` (registration bookkeeping: `Use` after registering a
  route, native middleware position, `UseNative`, a second `Next`, no re-entry after abort, native
  errors propagating, one allocation per request across nested groups). `make test`,
  `make test-race` and `make lint` passed for every adapter.

### A pre-existing difference found during implementation

`TestMiddlewareChainErrorFromInnerLayer` exposed a cross-adapter difference unrelated to this
change: ginx and hertzx always have an error handler configured, so an error returned by an inner
middleware is rendered at the failing layer and an outer middleware reads the final status code
after `Next`; echox and fiberx, with no adapter-level error handler configured, hand the error
back to echo's `HTTPErrorHandler` / fiber's `ErrorHandler`, and the response is written only
after the whole chain has unwound, so an outer layer still reads 200. The final response is the
same on all four adapters; only the timing of rendering differs. The test distinguishes the
assertion per adapter and records why. echox and fiberx error timing was **not** changed to make
an assertion pass; unifying it would be a separate change.

## 7. Where the remaining gap is: the cliff mechanism and the middleware signature

### Why Middleware20 is still 4x off

Measuring layer by layer from 16 to 26
(`fusion-cliff-layer-by-layer-stats.txt`, n=5):

| Layers | native ns/op | httpx ns/op | Difference |
| ---: | ---: | ---: | ---: |
| 16 | 94.2 | 130.6 | +39% |
| 19 | 103.7 | 128.3 | +24% |
| 20 | 108.2 | **396.5** | +266% |
| 21 | 112.5 | 402.7 | +258% |
| 22 | 116.6 | 403.3 | +246% |
| 23 | **366.6** | 409.5 | +12% |
| 24 | 389.1 | 411.3 | +6% |
| 26 | 408.0 | 423.2 | +4% |

The two curves have the same shape; httpx's cliff is simply at 20 layers and native's at 23.
`Middleware20` falls exactly inside that three-layer window: **the 4x is not per-layer overhead,
it is a three-layer difference in where the cliff lands.** The same implementation is +24% at 19
layers and +4% at 26. Benchmarking at 20 layers maximizes the apparent gap.

The mechanism was confirmed directly this round. Splitting `Next`'s cold path (tail driving,
error and abort handling) into a separate non-inlinable function made the frame smaller but added
one nested call in the tail, and the cliff moved **earlier**, to 19 layers (19 layers went from
126.6 ns to 389.2 ns). In other words: **one more nested frame = the cliff one layer sooner.**
That change was therefore not kept. The conclusion is that the cliff is determined by the number
of nested calls in the whole chain (compounded by frame size), and httpx spending slightly more
per layer than native buys it a wall three layers earlier. `(*ginChainContext).Next` has an
80-byte frame against 32 bytes for gin's own `(*Context).Next`; moving the cold path out does not
shrink it.

### PGO: only worth it in a single-framework deployment

Training a profile on `BenchmarkNestedScopes` plus gin's Middleware5/10 and JSON1K, compiling the
same source with `-pgo=off` and `-pgo=<profile>`, and alternating 6 rounds
(`fusion-pgo-stats.txt`):

| Scenario | PGO off | PGO on | Change |
| --- | ---: | ---: | ---: |
| Nested groups, 4 layers | 74.64 | 71.56 | -4.13% |
| 16 layers | 116.6 | 112.3 | -3.69% |
| 19 layers | 126.8 | 123.0 | -3.00% |
| 20 layers | 396.5 | **127.4** | -67.88% |
| 22 layers | 404.1 | 397.8 | -1.56% |

PGO pushes the cliff back about 2 layers (20 layers returns to the linear region); at ordinary
depths it is worth 3–4%, and allocation counts do not change. **It fixes precisely the
pathological point at 20 layers and helps very little at realistic depths.** A synthetic profile
is not a production gain, and this is not a default build setting for the library.

### The real structural lever: the middleware signature

The remaining gap comes from the shape of `httpx.Middleware`, not from the adapter.
`func(Context) error` plus `ctx.Next()` is the gin/hertz model: two calls and two frames per layer
(`Next` frame → middleware frame), plus a per-request object to hold the layer index. The
echo/chi shape `func(next Handler) Handler` — named `httpx.Interceptor` in this repository at the
time — composes into a closure tree at registration: one call and one frame per layer, and the
chain holds no mutable state at all.

Compared on the same gin dispatcher and the same ginx `Context`
(`BenchmarkMiddlewareModel`, n=6,
`fusion-middleware-model-stats.txt`):

| Layers | native gin | Middleware (then) | Interceptor form | vs native |
| ---: | ---: | ---: | ---: | ---: |
| 4 | 41.56 | 74.92 | **37.00** | -11% |
| 10 | 63.82 | 94.50 | **44.20** | -31% |
| 20 | 107.2 | 394.5 | **57.71** | -46% |
| 24 | 387.7 | 411.3 | **63.29** | -84% |
| 32 | 427.2 | 444.0 | **77.75** | -82% |
| 40 | 620.5 | 480.3 | **104.8** | -83% |

Interceptor: **zero allocations**, about 1.7 ns/layer, no cliff even at 40 layers, and **faster
than native gin** — the pre-composed closure chain skips gin's per-layer index bookkeeping and one
indirect handler call.

It also simplifies the semantics: not calling `next` is naturally an abort (no abort sentinel, no
layer index, no second-`Next` hazard). The cost is changing the middleware signature; the 9
middleware in `sphere/server` would need mechanical rewriting, and bridging a Next-style
middleware into a wrap chain still needs a per-request object (i.e. today's cost).

The comparison implementation covers the success path only. It has no per-layer native error
watermark and does not implement segmenting for "a native middleware in the middle"; this form
composes a whole run of middleware plus the handler into one native handler at route
registration, so a native slot can only sit outside the whole run, making the constraints on
mixing stronger than they are today. These numbers are an upper bound, not directly transferable.

## 8. The Interceptor form as shipped (experimental): the real gain is an order of magnitude smaller than the synthetic benchmark

The comparison in section 7 motivated an additional API, implemented and tested:
`httpx.Interceptor` (`func(next Handler) Handler`), the optional `InterceptorScope` capability
plus `httpx.UseInterceptor`, `ComposeInterceptors`, and conversions both ways, `AsMiddleware` /
`AsInterceptor`. What section 7 calls the wrap form is named Interceptor here.
`ginx.Router.UseInterceptor` composes the whole chain into the handler **at route registration**,
so there is no per-request state whatsoever. The logger, auth and permission middleware in
`sphere/server` were rewritten on top of Interceptor, with the existing `Log` /
`NewAuthMiddleware` / `NewPermissionMiddleware` unchanged (wrapped through `httpx.AsMiddleware`).

### Empty-layer chains: as section 7 predicted

`BenchmarkMiddlewareModel` re-measured against the shipped API (n=6,
`fusion-middleware-model-stats.txt`):

| Layers | native gin | Middleware | Interceptor | Interceptor allocs |
| ---: | ---: | ---: | ---: | ---: |
| 4 | 41.50 | 74.98 | **36.92** | 0 |
| 10 | 63.77 | 94.47 | **44.48** | 0 |
| 20 | 107.3 | 396.8 | **58.40** | 0 |
| 40 | 618.8 | 480.3 | **104.6** | 0 |

Zero allocations, about 1.7 ns/layer, no cliff at 40 layers, and 11% faster than bare gin at 4
layers.

### A real middleware stack: only 4%

> This subsection was measured while fusion was still in place. After fusion was removed,
> Interceptor is -10.6% with 4 fewer allocations; see [section 9](#9-fusion-was-removed-why).

But this is the decisive number. `BenchmarkRealStack`, added in `sphere/server/middleware`,
registers the real sphere-layout shape (access log + panic recovery + auth + permission, 4 layers
over 3 scopes) on one route with one response (n=6,
`fusion-real-stack-stats.txt`):

| Form | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: |
| No middleware (same route) | 30.80 | 0 | 0 |
| Middleware | 607.65 | 816 | 8 |
| Interceptor | **581.40** | 752 | 7 |

Interceptor is only **4.3%** faster, with one fewer allocation. The reason is direct: the four
real middleware do about 550 ns and 7 allocations of their own work, and chain dispatch is only
about 26 ns of it. The allocation profile
(`fusion-real-stack-allocs.txt`) attributes the
per-request allocations to `net.IP.String` (the logger calling `ClientIP()`), `context.WithValue`
plus `Request.WithContext` (auth storing auth data), the logger's attrs slice,
`net/textproto.canonicalMIMEHeaderKey` (the logger reading User-Agent), the claims roles slice,
and the one chain object of the Next-style form.

**This corrects the impression section 7 gives**: Interceptor is worth 2x on empty chains but
only 4% on a real middleware stack, because the bottleneck is what the middleware themselves do
(structured-log attrs, context copying, header canonicalization), not httpx's dispatch. Pushing
that 600 ns down further means optimizing the middleware — for instance, the logger not calling
`ClientIP()`/`Header()` on every request, and auth merging its two context writes — not further
work on the adapter layer.

### Behavioural differences (pinned as tests)

- Interceptor **always** runs inside middleware registered with `Use`/`UseNative` on the same
  scope, regardless of call order (`TestWrapOrderAndInheritance`).
- An error returned by an inner layer is rendered at the **route**, not in place at the failing
  layer (`TestWrapErrorReachesOuterLayerAndRendersOnce`) — the opposite of `Use`. The rewritten
  `logger` therefore logs the status code carried by the error rather than only reading
  `ctx.StatusCode()`, which incidentally fixed access logs recording failed requests as 200 on
  echox and fiberx.
- Not calling `next` is an abort; no abort bookkeeping, layer index or second-`Next` guard is
  needed (`TestWrapStopsChain`).
- `HandleStd` / `Static` are not inside the composed chain.
- Adapters without `InterceptorScope` (echox, fiberx, hertzx) fall back through `AsMiddleware`
  with identical behaviour (`TestMiddlewareChainComposedForm` / `ComposedStop` pass on all four)
  at the same cost as `Use`.

### Recommendation

On this data, Interceptor is **not worth pushing as a breaking refactor**: what it delivers is
three properties — zero allocations, no depth cliff, and empty chains faster than native — while
being worth 4% on a real stack. The right position is to keep it as an additional experimental
API that newly written hot-path middleware can use directly, with `sphere/server`'s middleware
already implemented on top of it (public API unchanged), and decide about migrating callers later.

The sphere changes use httpx symbols that are not yet released, so httpx has to be published
before `sphere/go.mod` can be raised. Local verification went through sphere-layout's `go.work`
(`GOWORK=.../sphere-layout/go.work`), without introducing a `go.work` into the sphere repository
(its CLAUDE.md forbids one).

## 9. Fusion was removed: why

Fusion (sections 4–6) ran in ginx for a while and was ultimately **deleted**. What remains is
`httpx.Interceptor` — today's `httpx.Middleware`.

The decision rests on putting this data side by side. The native / per-layer / Interceptor
columns below come from **one** sampling run (the code with fusion removed,
`BenchmarkMiddlewareModel`, n=6,
`fusion-removed-middleware-vs-interceptor-stats.txt`); the fusion column comes
from the run before the deletion and cannot be subtracted from the other three — it is there for
magnitude only:

| gin layers | native | per-layer (then current) | Interceptor | fusion (deleted, other run) |
| ---: | ---: | ---: | ---: | ---: |
| 4 | 41.7 | 91.1 | **38.9** | 76.1 |
| 10 | 66.0 | 424.6 | **44.1** | 96.1 |
| 20 | 107.6 | 600.7 | **57.9** | 400.3 |
| 40 | 624.4 | 1373.0 | **105.5** | 480.3 |
| allocs/op | 0 | 1 per layer (16 B) | **0** | 1 per request (64 B) |

At 4 layers Interceptor is indistinguishable from native gin (p=0.058).

Real middleware stack (access log + recovery + auth + permission, `BenchmarkRealStack`,
re-measured after the deletion, n=6, `fusion-removed-real-stack-stats.txt`):

| Form | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: |
| No middleware (same route) | 30.66 | 0 | 0 |
| Per-layer (then current) | 620.25 | 816 | 11 |
| Interceptor | **554.35** | 752 | **7** |
| Fusion (deleted, other run) | 604 | 816 | 8 |

**The middleware's own work is about 520 ns**, the overwhelming majority; changing the execution
form is worth at most 10.6%. This also corrects section 8's "only worth 4%": fusion was still in
place then and had already absorbed part of the gap. With fusion removed, Interceptor is
**-10.6% and 4 fewer allocations** against the then-current form.

Therefore:

1. **Fusion is dominated by Interceptor.** Given the willingness to change the middleware
   signature, Interceptor is faster at every depth, allocates nothing, and has no depth cliff.
   Fusion's only exclusive value was "you do not have to touch the call sites".
2. **Fusion's cost is not lines of code, it is one correctness risk.** A native middleware inside
   a segment drives the chain with `gc.Next()` and skips the rest of the segment. A bridge
   produced by `AdaptGinMiddleware` can be recognized by its method value's stable code pointer
   and given its own slot, but **a user's hand-written equivalent cannot be, and silently
   reorders** — which is why a `WithoutMiddlewareFusion` escape hatch was needed at all. Trading a
   correctness risk for 3.5% on a real stack does not hold up.
3. Interceptor has no such hole structurally: the chain is composed at the route, native
   middleware is always outside it, and the order cannot be scrambled. It also needs no abort
   bookkeeping, layer index or second-`Next` guard.

### What was deleted, what was kept

Deleted: `ginx/chain.go` (135 lines, including the reflect code-pointer sentinel
`isNativeBridge`), the `fuse`/`chain`/`slot`/`afterChange`/`useOne` bookkeeping in `Router` (about
60 lines; `Use` went back to 3), `Engine`'s root Router and 404/405 refresh hook,
`WithoutMiddlewareFusion`, and the allocation tests that existed only for fusion.

Kept: `UseNative` (no adapter layer at all — better than `AdaptGinMiddleware` with or without
fusion), the `ginBridge` method value, all of `httpx.Interceptor`, and
`conformance/middleware_chain_conformance_test.go` plus `ginx/middleware_test.go`, which test
contract behaviour — order, abort, error propagation, group inheritance, the 404 chain —
independent of the implementation.

### Known boundaries of Interceptor

Interceptor composes into **registered routes**, so it does not cover `Static`/`StaticFS`/
`HandleStd` or unmatched paths. Engine-level concerns that must cover static assets and 404s
(access log, panic recovery, CORS) should keep using `Use`. sphere-layout is split along exactly
that line: route-level auth, session, permission and rate limiting go through Interceptor, and
the three engine-level concerns stay on `Use`.

> Superseded: with one composed chain per route, the engine scope's chain now covers 404 and 405
> as well. That is what made collapsing the two middleware forms into one possible. See
> `CLAUDE.md`.

### Knock-on changes

All 8 middleware in `sphere/server` are now implemented on top of Interceptor, with the exported
`Middleware` versions wrapped through `httpx.AsMiddleware` and their signatures and behaviour
unchanged: `logger.LogInterceptor`, `logger.RecoveryLogInterceptor`, `auth.NewAuthInterceptor`,
`auth.NewPermissionInterceptor`, `cors.NewCORSInterceptor`,
`ratelimiter.NewRateLimiterInterceptor`, `ratelimiter.NewRateLimiterByClientIPInterceptor`,
`online.(*Online).Interceptor`, `selector.NewSelectorInterceptor`.

### Defects caught once the shared suite was filled in

Moving chain semantics, mixing, complex requests and edge cases into `httpxtest` (four adapters
running one contract) immediately exposed and fixed four:

| Defect | Impact | Fix |
| --- | --- | --- |
| The `httpx.AsMiddleware(httpx.AsInterceptor(mw))` shim passed *itself* downstream, so a downstream `ctx.Next()` recursed back into the shim and returned early — **the handler was silently skipped** | Converting between the two forms broke the chain, with no error and no log | The shim passes the adapter's own Context downstream |
| echox, fiberx and hertzx did not implement `InterceptorScope`, so `UseInterceptor` fell back to ordinary middleware | The documented "Interceptor always runs inside `Use`" held on ginx only; the same registration code executed in a different order on each adapter | All three implemented it as composition at route registration, unifying the ordering rule |
| echox and fiberx leaked the internal normalization key `"*"` from `Params()` | `Param("filepath")` agreed across all four, but code iterating `Params()` saw an extra key on echo and fiber | Drop `"*"` when a named alias exists; anonymous `/*` routes keep it |
| fiberx did not decode paths by default, so `Param` returned `hello%20world` where the other three returned `hello world` | Handlers using a path parameter as a filename or ID behaved differently | Set `UnescapePath: true` when fiberx builds the app; an app passed via `WithEngine` is the caller's to configure, now documented |

One **test-harness** limitation is also worth recording (not an adapter defect): fiber's
`app.Test` writes `ContentLength = -1` through verbatim as `Content-Length: -1`, which fasthttp
then rejects. "Request body with no declared length" is therefore declared through
`Caps.InProcessUnknownLengthBody` and skipped on fiberx with the reason recorded — fiber's real
socket path handles chunked bodies correctly.

## Reproducing

```sh
# the post-merge comparison (four frameworks x seven scenarios x native/httpx)
make bench-adapter > adapter.txt
benchstat -row /framework,/scenario -col /mode adapter.txt

# the real nested-group shape
go test ./ginx -run '^$' -bench '^BenchmarkNestedScopes$' \
  -benchtime=400ms -count=6 -cpu=4 -benchmem

# depth curves: gin, framework-free pure chain, pre-grown stack, frame size
go test ./ginx -run '^$' \
  -bench '^BenchmarkGinDepthCurve$|^BenchmarkChainDepthCurve$|^BenchmarkGinDepthCurveGrownStack$|^BenchmarkChainFrameSizeCurve$' \
  -benchtime=300ms -count=6 -cpu=4 -benchmem

# chain semantics and registration bookkeeping
go test ./conformance -run TestMiddlewareChain -count=1
go test ./ginx -run TestChain -count=1
```

The raw sample files named below are **not in the repository**: `benchmarks/` is part of the root Go module, so anything tracked there ships in the module zip every consumer of `github.com/go-sphere/httpx` downloads, and benchmark samples do not belong in a library's module zip. The tables in this report are the record; the filenames are kept so anyone who still has a local `benchmarks/results/` can find the matching output.

Raw data. Prototype stage: `fusion-prototype.txt`,
`fusion-prototype-stats.txt`,
`fusion-realistic-scopes.txt`,
`fusion-realistic-scopes-stats.txt`,
`fusion-pooled.txt`,
`fusion-depth-gin.txt`,
`fusion-depth-gin-stats.txt`,
`fusion-depth-grown-stack.txt`,
`fusion-depth-pure-chain.txt`,
`fusion-frame-size.txt`,
`fusion-environment.json`. After merging:
`fusion-adapter-after.txt`,
`fusion-adapter-after-stats.txt`,
`fusion-nested-scopes-after.txt`,
`fusion-nested-scopes-after-stats.txt`,
`fusion-depth-gin-after.txt`,
`fusion-depth-gin-after-stats.txt`.

Reproducing section 7:

```sh
# locating the cliff layer by layer (19-24 on the curve)
go test ./ginx -run '^$' -bench '^BenchmarkGinDepthCurve$' \
  -benchtime=300ms -count=6 -cpu=4

# middleware signature comparison (native / Middleware / Interceptor)
go test ./ginx -run '^$' -bench '^BenchmarkMiddlewareModel$' \
  -benchtime=300ms -count=6 -cpu=4 -benchmem

# PGO: train, rebuild, alternate
go test -c ./conformance -pgo=off -o /tmp/base.test
/tmp/base.test -test.run='^$' -test.cpuprofile=/tmp/train.pprof -test.benchtime=1s -test.cpu=4 \
  -test.bench='^BenchmarkNestedScopes$|^BenchmarkAdapter$/^framework=gin$/^scenario=(Middleware5|Middleware10|JSON1K)$'
go test -c ./conformance -pgo=/tmp/train.pprof -o /tmp/pgo.test
```

Reproducing section 8:

```sh
# empty-layer chains: native / Middleware / Interceptor
go test ./ginx -run '^$' -bench '^BenchmarkMiddlewareModel$' \
  -benchtime=300ms -count=6 -cpu=4 -benchmem

# the real middleware stack (in the sphere repository, with the workspace pointing at local httpx)
GOWORK=../sphere-layout/go.work go test ./server/middleware/ -run '^$' \
  -bench '^BenchmarkRealStack$' -benchtime=300ms -count=6 -cpu=4 -benchmem

# Interceptor semantics
go test ./ginx -run TestInterceptor -count=1
go test ./conformance -run TestMiddlewareChainInterceptor -count=1
```

Section 8 data: `fusion-real-stack.txt`,
`fusion-real-stack-stats.txt`,
`fusion-real-stack-allocs.txt`,
`fusion-middleware-model-stats.txt`.

Section 7 data: `fusion-cliff-layer-by-layer.txt`,
`fusion-cliff-layer-by-layer-stats.txt`,
`fusion-middleware-model.txt`,
`fusion-middleware-model-stats.txt`,
`fusion-pgo-off.txt`, `fusion-pgo-on.txt`,
`fusion-pgo-stats.txt`.

The prototype's `BenchmarkProtoChain` / `BenchmarkProtoRealisticScopes` /
`BenchmarkProtoPooledScopes` were deleted once the approach merged, since the library itself then
provided the paths they measured. The depth and frame-size diagnostics were kept as gin-specific
diagnostics and moved to `ginx/middleware_depth_benchmark_test.go` (going through the real ginx
registration path); `BenchmarkNestedScopes` and `BenchmarkMiddlewareModel` moved to `ginx/` too.

Samples run in Go benchmark declaration order, not randomized. In-process measurements exclude the
network client, TCP, databases and RPC, request binding and large-file paths. Samples from
different rounds cannot be mixed to compute a gain.
