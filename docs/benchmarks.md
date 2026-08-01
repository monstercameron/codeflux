# CodeFlux Performance Benchmarks

This document is the Git-tracked record required by `M22-090`. Benchmark
results are repository facts about the code at a revision, not application
state, so they live here and never in the runtime database.

Milestone: `M22-076` through `M22-090` in [`TODOS.md`](../TODOS.md).
Harness: [`internal/benchmarks`](../internal/benchmarks).

## Methodology

Every measurement is a Go benchmark. The registry in
`internal/benchmarks/registry.go` binds each `M22` TODO to the benchmark
function and package that satisfies it, and
`TestBenchmarkRegistryMatchesTheRepository` verifies each one exists in the
tree. A renamed or deleted benchmark fails the test suite rather than leaving a
requirement quietly unmet.

Rules the harness enforces:

- **Setup is never measured.** `benchmarks.Measure` stops the timer while a
  fixture is built and restarts it only around the body. Counting fixture
  construction is the most common way a benchmark reports a number for
  something other than what it claims.
- **Scale sweeps use the plan's sizes.** Where `docs/plan.md` names sizes
  (repository map at small/medium/large, replay at 100/1,000/10,000), the
  benchmark runs at each, and the registry records the required scales so a
  single-size run cannot pass as coverage.
- **Results are checked, not assumed.** Each benchmark asserts its output is
  non-empty and correctly sized. A benchmark measuring an empty result reports
  an impressive and meaningless number.
- **Tail latency is sized against the clock.** `benchmarks.ClockGranularity`
  measures the smallest interval this platform's monotonic clock can report,
  and the latency benchmark sizes its sampling window to span that many times
  over. On the reference machine below the granularity is ~65µs–500µs, far
  coarser than a single event append, so per-append timing would report zeros.
  The measured granularity is published as a metric beside every tail figure.

### Recorded dimensions (`M22-088`)

Every benchmark reports:

| Dimension | Metric | Source |
| --- | --- | --- |
| wall time | `wall-ns/op` | measured around the body |
| CPU | `cpu-s` | elapsed process time for the measured window |
| memory | `live-heap-B` | `runtime.MemStats.HeapAlloc` after the run |
| allocation | `alloc-B/op`, `mallocs/op` | `MemStats` delta, plus `-benchmem` |

`live-heap-B` deliberately reports `HeapAlloc` rather than `HeapSys`. `HeapSys`
is everything the runtime has reserved from the OS across the whole test
binary, which in a package with many benchmarks reports gigabytes unrelated to
the benchmark being read.

### Running them

```
go test ./internal/benchmarks/ -run '^$' -bench . -benchmem
go test ./internal/graphlayout/ -run '^$' -bench 'BenchmarkM19' -benchmem
```

Output is retained under `.artifacts/bench/` and is not committed; the summary
below is.

## Environment (`M22-089`)

The plan targets an ordinary hobbyist laptop.
`benchmarks.CaptureEnvironment` records the machine with every run and
classifies it, so a result produced on something larger is never presented as
representative.

| Field | Reference run |
| --- | --- |
| OS / architecture | `windows/arm64` |
| Go | `go1.26.3` |
| Logical CPUs | 18 |
| Class | `above-target` |
| Clock granularity | ~65µs (varies by run; published per benchmark) |

**The reference numbers below were produced on an `above-target` machine.** A
target-class laptop should be expected to be slower. The classification is
emitted by every benchmark's log line precisely so this caveat cannot be lost
when a number is quoted.

## Results

Reference run, `-benchtime 300ms`, one machine, one revision. These are a
baseline for comparison, not a promise.

| TODO | Subject | Benchmark | Result |
| --- | --- | --- | --- |
| `M22-076` | Cold coordinator startup | `BenchmarkColdCoordinatorStartup` | 279 ms/op, 1.79 MB, 7,879 allocs |
| `M22-077` | Warm coordinator startup | `BenchmarkWarmCoordinatorStartup` | 16.1 ms/op, 1.19 MB, 2,047 allocs |
| `M22-078` | Migration from prior schema | `BenchmarkMigrationFromPriorSchema` | 45.6 ms/op, 524 KB, 3,523 allocs |
| `M22-079` | Repository map (small, 5 pkgs) | `BenchmarkRepositoryMap` | 200 ms/op, 389 KB, 1,787 allocs |
| `M22-079` | Repository map (medium, 32 pkgs) | `BenchmarkRepositoryMap` | 287 ms/op, 1.82 MB, 19,571 allocs |
| `M22-079` | Repository map (large, 202 pkgs) | `BenchmarkRepositoryMap` | 680 ms/op, 16.9 MB, 214,733 allocs |
| `M22-080` | Context selection | `BenchmarkContextSelection` | 29.7 ms/op, 2.14 MB, 24 items |
| `M22-081` | Event append | `BenchmarkEventAppend` | 109 ns/op, 9.15M appends/s, p50 122 ns, p99 161 ns, max 184 ns |
| `M22-082` | Reconnect replay (100) | `BenchmarkReconnectReplay` | 4.81 µs/op |
| `M22-082` | Reconnect replay (1,000) | `BenchmarkReconnectReplay` | 48.1 µs/op |
| `M22-082` | Reconnect replay (10,000) | `BenchmarkReconnectReplay` | 479 µs/op |
| `M22-083` | Thread initial render | `BenchmarkThreadRenderAndPagination` | 4.34 µs/op, 10 KB, 4 allocs |
| `M22-083` | Upward pagination (1,100 msgs) | `BenchmarkThreadRenderAndPagination` | 274 µs/op, 561 KB, 1,112 allocs |
| `M22-084` | Token + cost updates | `BenchmarkSimultaneousTokenAndCostUpdates` | 137 ns/op, 7.29M pairs/s |
| `M22-085` | 300-node graph layout | `BenchmarkM19Initial300NodeLayout` | 973 µs/op, 768 KB, 7,296 allocs |
| `M22-086` | 100 graph patches | `BenchmarkM19Sequential100GraphPatches` | 148 ms/op, 1.48 ms/patch |
| `M22-087` | Vector search (2,000 × 384d) | `BenchmarkVectorSearchAtPrototypeScale` | 1.56 ms/op, 65.6 KB, 4 allocs |

### What these say

- **Replay is linear in backlog** (4.8 µs → 48 µs → 479 µs across 100× input),
  which is the property that matters: a long disconnect costs proportionally,
  not quadratically.
- **Cold startup dominates warm by ~17×**, and the gap is almost entirely
  migration plus first-open work. A user pays it once per install.
- **The repository map is the most expensive local operation** and grows
  faster than package count alone (3.4× time for 40× packages, but 43× the
  allocations). Allocation volume is the thing to watch if it regresses.
- **Upward pagination is ~60× the cost of an initial render** because it
  re-merges the whole feed. That is acceptable at 1,100 messages and is the
  number to re-measure before raising the page limit.
- **Vector search at prototype scale is ~1.6 ms** for 2,000 candidates. This is
  recorded even though embedding discovery is an opt-in branch that is not yet
  justified, because the cost has to be known before the branch is taken.

### Defect found by this work

`BenchmarkThreadRenderAndPagination` failed on first run with a feed of 51
messages where 1,100 were expected. The cause was a real bug in
`web/frontend/timeline/client.go`: `cloneMessageFeed` reassigned
`feed.Messages` to a fresh slice *before* ranging over it, so it copied the new
empty slice onto itself and replaced every loaded message with a zero value.
Every upward pagination would have collapsed the visible thread to a single
blank row. It is fixed, and
`TestBeginOlderMessagePagePreservesLoadedMessages` guards it — the existing
begin/exhausted test used a feed with no messages and could not see it.
