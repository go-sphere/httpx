# Gin deep middleware chain: diagnosis

2026-09-18, Apple M1 Pro, Go 1.27.1, GOMAXPROCS=4. In the previous round a 10-layer chain went
from 63.69 ns native to 466.75 ns through httpx (+632.85%) — a real, unresolved performance
problem. Re-sampling still gave about 430 ns. "It is only a few hundred nanoseconds in absolute
terms" is not an argument that the work is done.

> Historical. The design this report measures — one native handler per httpx middleware layer —
> is gone: middleware is composed into the route at registration now. The curves in
> [../BENCHMARK.md](../BENCHMARK.md) section 5 are the current numbers. What is kept here is the
> attribution of the cliff and the reasoning about PGO, neither of which a re-run produces.

## What was happening

The recursive call path for a normal request:

```text
native: Gin.Next -> user gin middleware -> Gin.Next -> ...
httpx:  Gin.Next -> adapter func -> user httpx middleware -> httpx.Next -> Gin.Next -> ...
```

On top of that, gin allocated a 16-byte wrapper per layer to record whether that layer had
called Next. The previous round removed the wrapper allocation for the business handler only;
it did not remove these middleware allocations.

The native benchmark and httpx use the same gin version, the same dispatcher and the same
204-response scenario. Checked: this is not a network call compared against an in-process one,
and no fiber/hertz code path is mixed in.

## Control experiment

`BenchmarkGinCallDepth` (now in `ginx/call_depth_benchmark_test.go`) adds, to the *native*
chain, either two non-inlinable forwarding functions, or one escaping 16-byte object, or both.
Six samples of 200ms each; the table is median ns/op.

These shapes are diagnostic only: the allocation is forced to escape through a serial benchmark
sink and does not have exactly the lifetime of the real wrapper, so the columns cannot simply be
subtracted to price an individual operation.

| Layers | native | +forwarding only | +per-layer alloc only | forwarding + alloc | httpx then |
| --- | ---: | ---: | ---: | ---: | ---: |
| 5 | 44.62 | 49.10 | 115.70 | 119.60 | 129.15 |
| 8 | 55.63 | 61.06 | 151.90 | 160.40 | 174.05 |
| 9 | 59.62 | 65.35 | 168.05 | 380.55 | 302.75 |
| 10 | 63.77 | 69.33 | 182.90 | 411.90 | 430.80 |
| 15 | 85.25 | 364.55 | 256.25 | 486.45 | 513.20 |
| 20 | 108.25 | 579.80 | 535.25 | 763.65 | 615.00 |

This supports the reading that recursive forwarding depth and per-layer allocation compound into
a non-linear cost: adding the same kinds of overhead to *native* code reproduces the same jump.
Even with no allocation at all, the extra forwarding makes 15- and 20-layer chains clearly
slower. So pooling the wrapper, or merely shrinking it, would not necessarily fix deep chains.

The CPU profile shows `Gin.Next`, the adapter function, `httpx.Next`, the allocator, and GC and
scheduler work. **No hardware performance counters were collected, so the jump cannot be
attributed precisely to return-address prediction, cache behaviour or any hardware threshold.**
Nor does this guarantee the jump lands at the same depth on another CPU or Go version.

## Optimizations tried

Three small source changes were tested through a temporary build overlay and not written into
the production implementation: moving error handling out of the hot branch, forbidding inlining
of the registration factory, and constructing the wrapper directly with a smaller stack frame.
Across two preliminary samples all three still sat at about 430 ns at 10 layers — not enough to
justify merging.

Separately, Go PGO was trained on 1/5/10/20-layer samples covering both native and httpx, and
the same source was then compiled with PGO off and on. Six alternating runs:

| Layers | httpx, normal build | httpx, PGO build | Change | native, PGO build | httpx allocs |
| --- | ---: | ---: | ---: | ---: | ---: |
| 1 | 49.03 | 45.41 | -7.39% | 29.63 | 1 → 1 |
| 5 | 128.85 | 124.80 | -3.14% | 41.71 | 5 → 5 |
| 10 | 432.85 | 201.20 | -53.52% | 60.59 | 10 → 10 |
| 20 | 620.75 | 583.85 | -5.94% | 103.85 | 20 → 20 |

Every httpx difference above is p=0.002 (n=6). Disassembly confirms PGO devirtualized and
inlined the hot interface call, removing one layer of forwarding; 10 layers roughly halved but
stayed slower than native, and 20 layers gained little. The allocation count never dropped.

PGO preserves the API and the runtime semantics and requires no pooling, but this used a profile
from a synthetic benchmark. **It is not a promise of production gains and was not adopted as a
default build setting for the library.** A real service should profile its own traffic and then
verify throughput, latency and regressions. Official guidance: [Go PGO](https://go.dev/doc/pgo).

## Where further optimization would have to go

Meaningfully reducing this cost in the library means evaluating whether consecutive httpx
middleware can be composed into a single execution chain, adapted once at the framework entry
point, so each layer stops bouncing between gin and httpx. The public `Handler`/`Middleware`
signatures can stay the same, but identical signatures do not by themselves make the execution
behaviour compatible.

Such a change has to handle Next/Abort in native gin middleware, an outer layer reading state
after an error, native and unified middleware mixed on one scope, route group inheritance, the
standard-middleware bridge, and behaviour when a `Context` is retained. Bypassing `Gin.Next`
directly, or unconditionally aborting after each middleware returns, would both change existing
behaviour. This restructuring is not implemented or validated here; this round did not change
production code at the cost of native interoperability.

If you already have native gin middleware, register it on the gin engine you pass to
`ginx.WithEngine` and use httpx handlers for the routes, rather than converting native
middleware into httpx and back again. That uses the existing API, but it requires a native
implementation and is not a substitute for portable cross-framework middleware.

## Reproducing

```sh
# control experiment
go test ./ginx -run '^$' -bench '^BenchmarkGinCallDepth$' \
  -benchtime=200ms -count=6 -cpu=4 -benchmem

# synthetic profile, only to reproduce this report's PGO experiment
go test -c ./conformance -pgo=off -o /tmp/httpx-baseline.test
/tmp/httpx-baseline.test -test.run='^$' \
  -test.bench='^BenchmarkAdapter$/^framework=gin$/^scenario=Middleware(1|5|10|20)$' \
  -test.benchtime=1s -test.cpu=4 -test.cpuprofile=/tmp/httpx-depth.pprof
go test -c ./conformance -pgo=/tmp/httpx-depth.pprof -o /tmp/httpx-pgo.test
```

Then run the same benchmark against both binaries, swapping the order each round, and compare
with benchstat. Timings taken while profiling are not mixed with unprofiled samples.

The raw sample files named below are **not in the repository**: `benchmarks/` is part of the root Go module, so anything tracked there ships in the module zip every consumer of `github.com/go-sphere/httpx` downloads, and benchmark samples do not belong in a library's module zip. The tables in this report are the record; the filenames are kept so anyone who still has a local `benchmarks/results/` can find the matching output.

Data: `gin-call-depth.txt`,
`gin-call-depth-stats.txt`,
`gin-depth-pgo-off.txt`, `gin-depth-pgo-on.txt`,
`gin-depth-pgo-stats.txt`,
`gin-depth-cpu-top.txt`,
`gin-depth-environment.json`, plus the CPU and PGO profiles themselves.

This round added diagnostic benchmarks and this report; the production Go implementation was not
changed. Formatting, go vet, golangci-lint and nilaway passed for the conformance module the
diagnostics live in, and a race smoke check passed for all five 10-layer modes (race-mode
timings are not used for any performance conclusion).
