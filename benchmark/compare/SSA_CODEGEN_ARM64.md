# Shared SSA code generation on Apple M1 Pro

A single-basic-block SSA stage now replaces the amd64 peephole optimizer and
also runs before ARM64 instruction emission. Generated text is smaller, but
these measurements do **not** establish a stable warm encoding speedup. Cold
code generation costs about seven times as much as the previous direct ARM64
emitter. This is a foundation for further code-generation work, not evidence
that adding SSA alone makes the codec substantially faster.

## Scope and invariants

- Unique SSA definitions for register writes; explicit memory versions for
  stores, literal copies, helper calls and frame operations.
- Constant folding with 64-bit wrapping semantics, copy propagation, available
  common-expression/load elimination, and dead-value elimination.
- Propagation checks physical-register availability. Implicit helper argument
  uses remain pinned to ABI registers; helper calls invalidate availability.
- Every memory write invalidates previous load equivalence. No alias analysis,
  speculative loads, store elimination or instruction scheduling is attempted.
- Frame setup/teardown remain effects so saved context roots, FP/LR handling,
  stack growth and GC metadata survive optimization.

The generated functions currently have one basic block: branches and loops
remain inside Go helpers. This version has no CFG, phi nodes, inlining or loop
optimization. amd64 retains its allocator for surviving virtual scratch
registers; ARM64 retains fixed register homes. The direct Go decoder bypasses
this stage. See [the architecture description](../../ARCHITECTURE.md).

## Generated code

Native darwin/arm64, all six generated functions per fixture:

| Fixture | Before instructions | After instructions | Instruction change | Before bytes | After bytes | Size change |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| Book | 514 | 454 | -11.67% | 2,080 | 1,840 | -11.54% |
| TwitterResponse | 2,311 | 2,123 | -8.14% | 9,264 | 8,528 | -7.94% |

Book's SSA graph shrinks from 613 to 559 values; TwitterResponse shrinks from
2,601 to 2,404. Counts include input and memory/effect nodes. The ARM64 numbers
above count actual instruction words; byte sizes include alignment padding.
Code generation statistics are deterministic for these fixtures.

Useful removals include unused data/context reloads after the last helper,
repeated context loads within one memory version, copies whose source remains
available, and address additions of zero. A nonadjacent `Const(10); Add(5);
Sub(3)` chain can become `Const(12)` when its result is used. The previous
adjacent peephole pass could only combine the two arithmetic instructions.
Helper calls, literal writes and stack-frame effects remain.

Smaller text does not prove fewer instruction-cache misses or better branch
prediction. No hardware counter claim is made here. The main remaining runtime
work still happens inside Go helpers, which SSA does not optimize.

## Warm encoding measurements

Baseline `d22c43d`, final runtime `59d3414`, measured on 2026-11-23 with Apple
M1 Pro, macOS 27.0.1 and Go 1.25.0. Six 300 ms samples per case, medians below.
Each round alternates baseline/final process order. All binaries were built
before timing. Suites run sequentially, with `GOMAXPROCS=1` and `-cpu=1`.
There were no concurrent builds, tests, fuzzers or other benchmark processes.
Normal desktop activity was not isolated.

These runs use a different CPU configuration from earlier README tables; compare
within the table, not directly against historical results. This experiment
compares compiler revisions of jjs-go, not library rankings.

| Case | Before ns/op | After ns/op | Change | Faster paired rounds |
| --- | ---: | ---: | ---: | ---: |
| Encoder / Binding / Small | 902.95 | 906.85 | +0.43% | 3/6 |
| Encoder / Binding / Medium | 7,807.00 | 7,708.50 | -1.26% | 3/6 |
| Encoder / Binding / Large | 332,539.00 | 327,653.50 | -1.47% | 4/6 |
| Encoder / Generic / Small | 2,975.00 | 2,973.00 | -0.07% | 3/6 |
| Encoder / Generic / Medium | 30,407.00 | 30,432.00 | +0.08% | 3/6 |
| Encoder / Generic / Large | 1,487,382.50 | 1,479,344.00 | -0.54% | 3/6 |
| Hotpath / DynamicFields | 141.70 | 142.10 | +0.28% | 3/6 |
| Hotpath / Interfaces | 459.35 | 471.25 | +2.59% | 2/6 |
| Hotpath / BufferReuse | 76.37 | 76.38 | +0.02% | 2/6 |

All nine warm cases retain their original B/op and allocs/op. Binding uses
608 / 9,616 / 282,264 B/op and 2 / 2 / 210 allocs/op; Generic uses 1,344 /
16,768 / 717,844 B/op and 11 / 77 / 2,529 allocs/op. BufferReuse remains 0 B/op
and 0 allocs/op.

The final round medians range from -1.47% to +2.59%, with no case faster in more
than four of six paired rounds. Within-case `(max-min)/median` reaches 24.5%
for Binding Medium and 92.0% for BufferReuse. All samples are retained. The
initial exploratory run showed roughly 4% Binding Medium/Large improvements,
but the final round did not reproduce that magnitude. Both raw experiments are
included; there is no robust throughput conclusion from these noisy samples.

## Cold generation cost

`BenchmarkJITCodegen` builds a type frontend, runs optimization and assembles
code, without loading executable memory or registering runtime modules. The
same benchmark source was copied into a detached baseline checkout. This
isolates generation cost and avoids accumulating permanent JIT registrations.

| Struct | Before | After | Ratio | B/op before → after | Allocs/op before → after |
| --- | ---: | ---: | ---: | ---: | ---: |
| 4 integer fields | 3.915 µs | 28.413 µs | 7.26× | 5,280 → 59,690 | 53 → 85 |
| 32 integer fields | 25.993 µs | 181.270 µs | 6.97× | 37,385 → 420,035 | 205 → 245 |

The first implementation scanned every available register during propagation.
Indexing definitions by value number reduced the initial roughly 46 / 319 µs
costs for these cases, while preserving the final machine instruction counts.
SSA graph construction, cloning, numbering and liveness still add allocation
and CPU cost relative to direct machine-word emission. Statistics also lower
an unoptimized copy to count backend instructions; that cost is included.

Compilation is cached per concrete type, so this overhead is normally paid
once per type. Workloads creating many distinct types pay it repeatedly. The
warm measurements exclude compilation and must not be used to hide this cost.
Before treating SSA as a broad performance win, reduce graph/allocation costs
and expose more helper work to optimization. A CFG/phi implementation is only
needed once control flow moves into generated functions.

## Validation and reproduction

- Native `go test ./...`, compiler race tests, and comparison-module tests pass.
- Tests cover constant overflow, overwritten register homes, implicit ABI
  operands, store/call/copy barriers and dead context loads.
- 300 deterministic random programs, each with 150 operations, compare physical
  register/memory behavior before and after optimization.
- Existing helper argument-spill, stack-growth/GC and concurrent codec tests pass.
- `FuzzJITMarshalString` with `-tags jitfuzz` passed a 10-second run with 20,814
  executions (including differential JSON and round-trip checks).
- [All 15 CI jobs for runtime 59d3414 passed](https://github.com/nyan233/jjs-go/actions/runs/45127964743),
  including Windows amd64 Go 1.22/1.25, Linux amd64, macOS, ARM64 assembly and
  QEMU, and the Go 1.21–1.27 compiler/loader matrix.

```sh
# Generated instruction/text statistics
cd benchmark/compare
go run ./cmd/codegenstats

# Warm comparison cases: build before timing and alternate baseline/final runs.
go test -c -o /tmp/jjs-ssa-compare.test .
GOMAXPROCS=1 /tmp/jjs-ssa-compare.test -test.run='^$' \
  -test.bench='^BenchmarkEncoder_(Binding|Generic)$/(Small|Medium|Large)/jjs-go$' \
  -test.benchmem -test.benchtime=300ms -test.count=1 -test.cpu=1

# From repository root: cold generation, no executable-module loading.
cd ../..
GOMAXPROCS=1 go test ./core/compiler -run '^$' \
  -bench '^BenchmarkJITCodegen$' -benchmem -benchtime=300ms -count=6 -cpu=1
```

[Complete raw outputs](SSA_CODEGEN_ARM64_RAW.txt) include all six samples in
both experiments and the baseline/final generated-code statistics.
