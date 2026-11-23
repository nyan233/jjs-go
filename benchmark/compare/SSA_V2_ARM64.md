# SSA after typed JIT inlining: ARM64 reassessment

**Recommendation: keep this experiment off main.** Static code shrinks, but
steady-state encoding shows no broad, repeatable gain and compilation becomes
substantially slower. This conclusion is about the implemented machine-SSA
passes, not a claim that a richer typed SSA could never help.

This experiment starts at main `2813ca6`, after the primitive, slice, Base64,
map and direct-quote optimizations. Runtime implementation: `6c7194b` on
`codex/experiment-ssa-v2`. The earlier single-block SSA experiment remains
on `codex/experiment-ssa`; neither experiment is merged to main.

## What is being evaluated

The ARM64 encoder lifts the emitted scalar instructions into machine SSA,
builds basic blocks and predecessor edges, and assigns distinct definitions
to general registers, NZCV, memory and the used floating-point register.
Lazy phi nodes represent joins and loop backedges, including an explicit
function-entry input when a backedge targets the entry block.

The pass performs constant folding, constant branch simplification, copy
propagation, redundant-load elimination, exact 64-bit store-to-load forwarding
and dead-definition elimination. Copy propagation checks that an equivalent
value still occupies an available physical register. One conservative memory
version invalidates loads on every store/call; overlapping partial stores
cannot forward a stale full-width value.

This machine-SSA experiment preserves physical register
homes and instruction order. It does not include loop-invariant code motion,
alias analysis, register reallocation, capacity-check fusion or store removal.
GC root stores, frame changes and calls are retained. Helper argument metadata
keeps actual Go ABI operands live; tests verify the signatures. Unknown scalar
operations and the handwritten Base64 kernel remain opaque. Unknown control
flow or entry into an opaque kernel disables optimization of that function.
Relative branches and stack-PC metadata are relocated after deletion.

SSA applies to ARM64 encoders in this branch. amd64 continues to use the
existing backend; decoder generation and NEON kernels are unchanged. Build
with `-tags jitnossa` to disable the pass and its call-site metadata recording.
The same-source disabled build is measured as an additional control.

## Measurement method

Apple M1 Pro (10 cores, 32 GiB), macOS 27.0.1, Go 1.25.0 darwin/arm64,
AC power, `GOMAXPROCS=10`, `SONIC_NO_ASYNC_GC=1`, measured 2026-11-23.
Prebuilt baseline and SSA binaries run sequentially for five rounds, alternating
which version goes first. No local builds, tests or profiling run concurrently
with timing. Steady-state samples last 300 ms and the tables use medians.
The machine remains a normal desktop, so small changes need caution.

The baseline binaries include the same added benchmark harness, with production
code from `2813ca6`. Encoder microbenchmarks reuse output capacity; comparison
benchmarks retain their existing Marshal API behavior and warm type caches.
Ordinary Generic does not normalize map-key sorting across codecs;
UnorderedGeneric is the matching unsorted comparison.

Cold measurements are separate: fixed 50-iteration runs compile and load a
fresh encoder every time, while `BenchmarkJITCodegen` emits and assembles fresh
four/32-integer-field types without registering executable modules. Both can
have warm reflection metadata and pooled SSA scratch space. They do not
measure a completely fresh process's first request. `sync.Pool` can discard
scratch on GC, which affects allocation totals and retained memory.

All 700 primary samples are retained in [main](ssa-v2-arm64-main.txt) and
[SSA](ssa-v2-arm64-final.txt). Another 65 samples cover the
[disabled control](ssa-v2-arm64-disabled.txt), and 40 cover the
[selected recheck](ssa-v2-arm64-recheck.txt). No samples were discarded.
[Metadata](ssa-v2-arm64-metadata.json) records the revisions and selectors.

## Generated code

| Fixture | Main instructions | SSA instructions | Main code B | SSA code B | Code change |
|---|---:|---:|---:|---:|---:|
| Book | 2,144 | 2,063 | 8,576 | 8,256 | -3.7% |
| Twitter | 6,875 | 6,649 | 27,520 | 26,608 | -3.3% |

Twitter still has two generated functions. The pass builds 862 basic blocks,
13,685 SSA nodes and 6,740 phi nodes, including pass-through block parameters.
It reports 419 rewrites and eliminates 226 machine instructions. Phi nodes are
analysis objects; the original register homes already implement the joins,
so no additional phi-copy instructions are emitted. Full diagnostic output
is in [ssa-v2-arm64-codegen-stats.txt](ssa-v2-arm64-codegen-stats.txt).

## Steady-state comparison

| jjs-go case | Main ns/op | SSA ns/op | Time change |
|---|---:|---:|---:|
| Binding/Small/jjs-go | 459.1 | 455.9 | -0.7% |
| Binding/Medium/jjs-go | 3,238 | 3,238 | +0.0% |
| Binding/Large/jjs-go | 135,209 | 133,105 | -1.6% |
| Generic/Small/jjs-go | 1,392 | 1,382 | -0.7% |
| Generic/Medium/jjs-go | 14,425 | 14,382 | -0.3% |
| Generic/Large/jjs-go | 749,178 | 752,947 | +0.5% |
| UnorderedGeneric/Small/jjs-go | 1,395 | 1,383 | -0.9% |
| UnorderedGeneric/Medium/jjs-go | 14,463 | 14,406 | -0.4% |
| UnorderedGeneric/Large/jjs-go | 750,063 | 751,548 | +0.2% |

Allocation counts are unchanged: Binding has two allocations/op; Generic and
UnorderedGeneric have one. Unchanged goccy/Sonic control medians vary from
-2.0% to +2.7% between the two primary runs. The
small jjs-go changes do not establish a general throughput improvement.

Five further rounds alternated the version order at 500 ms per case:

| Recheck | Main ns/op | SSA ns/op | Time change |
|---|---:|---:|---:|
| Binding/Large/jjs-go | 134,491 | 133,172 | -1.0% |
| Generic/Large/jjs-go | 750,514 | 751,580 | +0.1% |
| UnorderedGeneric/Large/jjs-go | 748,603 | 752,897 | +0.6% |
| Inline/Records | 8,827 | 8,740 | -1.0% |

The recheck leaves Binding/Large and Records around 1% faster, while both
Generic Large cases remain effectively flat. Earlier partial-run differences
of several percent did not persist through the completed measurements.

## Reused-buffer microbenchmarks

| Case | Main ns/op | SSA ns/op | Time change | Main code B | SSA code B |
|---|---:|---:|---:|---:|---:|
| Inline/Bool | 18.28 | 18.2 | -0.4% | 208 | 208 |
| Inline/IntSmall | 18.55 | 18.51 | -0.2% | 384 | 368 |
| Inline/IntLarge | 29.65 | 29.65 | +0.0% | 384 | 368 |
| Inline/Record | 83.1 | 83.8 | +0.8% | 1,664 | 1,584 |
| Inline/Pointer | 83.59 | 86.31 | +3.3% | 1,984 | 1,888 |
| Inline/Ints | 85.13 | 84.89 | -0.3% | 880 | 832 |
| Inline/Records | 8,830 | 8,755 | -0.8% | 2,144 | 2,064 |
| Inline/Bytes0 | 18.47 | 19.27 | +4.3% | 640 | 624 |
| Inline/Bytes12 | 24.49 | 24.44 | -0.2% | 640 | 624 |
| Inline/Bytes48 | 21.41 | 21.38 | -0.1% | 640 | 624 |
| Inline/Bytes1024 | 73.87 | 74.31 | +0.6% | 640 | 624 |
| Inline/Bytes65536 | 3,771 | 3,770 | -0.0% | 640 | 624 |
| Map/Nil | 3.359 | 3.336 | -0.7% | 1,264 | 1,168 |
| Map/Empty | 34.32 | 34.59 | +0.8% | 1,264 | 1,168 |
| Map/Small | 96.53 | 96.83 | +0.3% | 1,264 | 1,168 |
| Map/StringInt | 3,366 | 3,411 | +1.3% | 1,264 | 1,168 |
| Map/StringString | 3,834 | 3,854 | +0.5% | 1,200 | 1,120 |
| Map/IntUint | 2,657 | 2,583 | -2.8% | 1,392 | 1,280 |
| Map/Struct | 4,783 | 4,830 | +1.0% | 2,016 | 1,904 |
| Map/Nested | 3,870 | 3,841 | -0.7% | 2,128 | 1,968 |
| Map/Interface | 6,260 | 6,414 | +2.5% | 992 | 896 |
| Map/EscapedKeys | 324.6 | 328.9 | +1.3% | 1,200 | 1,120 |

All these warmed microbenchmarks retain zero allocations/op. Nil maps return
before generated map traversal. Base64's NEON body is opaque to SSA, so its
small timing changes are not evidence of a better SIMD algorithm.

## Compilation costs

| Workload | Main ns/op | SSA ns/op | SSA / main | Main B/op | SSA B/op | Main allocs/op | SSA allocs/op |
|---|---:|---:|---:|---:|---:|---:|---:|
| Generate only / Fields4 | 10,242 | 87,873 | 8.58× | 25,412 | 23,590 | 204 | 208 |
| Generate only / Fields32 | 75,110 | 666,607 | 8.88× | 205,610 | 295,444 | 1,285 | 1,296 |
| Compile + load / Book | 86,676 | 532,782 | 6.15× | 116,704 | 178,694 | 851 | 863 |
| Compile + load / Twitter | 270,841 | 1,582,982 | 5.84× | 405,758 | 820,693 | 2,404 | 2,434 |

Pure generation is 8.58–8.88× slower; complete Book/Twitter compilation is
6.15×/5.84× slower. Pooled flat arrays and cached value aliases limit allocation
counts, but do not remove graph construction, phi resolution, propagation and
liveness work. Pool misses/GC also cause allocation-byte variation.

The primitive/map cold cases show 2.19–6.24× slowdowns; all 17 case histories
are retained in the raw files. For example, a single bool encoder grows from
7.852 to 17.158 µs, a record-slice encoder from 19.298 to 120.491 µs, and nested
maps from 18.930 to 114.018 µs. Warm encode results exclude all of this cost.

The `jitnossa` control restores baseline code sizes (Book 8,576 B; Twitter
27,520 B). Its generation medians are 10.310/74.845 µs for Fields4/Fields32,
and complete compilation 89.313/258.367 µs for Book/Twitter. This supports
attributing the large regression to enabling the pass, rather than merely
adding benchmark files or helper-call annotations to the branch.

## Profiles and interpretation

Separate two-second CPU profiles cover pure Fields32 generation and warm
Binding/Large encoding for both binaries. Their top tables and timing output
are retained in [ssa-v2-arm64-profiles.txt](ssa-v2-arm64-profiles.txt). Profiled
runs are excluded from the timing medians above.

The generation profile exposes the added `optimizeARM64SSA`, `canonical`,
`constant`, `propagate`, graph-building and liveness paths. Warm profiles
continue to show the generated encoder, `quoteJSONString` and runtime/GC
work; the SSA optimizer runs only during compilation. These macOS profiles
contain substantial runtime scheduling and GC-thread samples, so their
percentages are not used as a wall-clock cost decomposition. A separate
initial full-compilation profile also contained substantial executable-memory
mapping work; the no-loading generation benchmark establishes that the
regression is not explained solely by module loading.

The reduced code mostly addresses register copies and redundant definitions.
The current memory model invalidates all prior loads on any store or call,
and fixed register homes limit propagation across control-flow joins. It
does not combine repeated capacity checks, move loop-invariant loads or
change the quote/Base64 kernels, integer conversion algorithm or map iterator.
That is why this experiment is not equivalent to a high-level typed SSA with
alias/effect analysis. The runtime measurements, rather than static instruction
counts alone, determine the recommendation to keep it experimental.

## Validation

- Native root/comparison suites and builds; Go 1.22.12, 1.25 and 1.26.0;
  ARM64 assembly checks and a build with `jitnossa`.
- Compiler race tests and a 20-second map fuzz run: 40,452 executions, pass.
- 500 deterministic random programs compare original/optimized register,
  memory and control-flow behavior with a separate scalar interpreter.
- Regression cases cover overwritten register homes, overlapping stores,
  loop phis, entry backedges, constant branches, opaque kernel entries,
  unsupported control flow and helper argument signatures.
- Existing forced GC, stack growth, helper spills, map mutation, quoting and
  Base64 boundary tests exercise the optimized generated functions natively.
- [Measured implementation CI](https://github.com/nyan233/jjs-go/actions/runs/45164877368)
  passed all 15 jobs, including Windows amd64, macOS ARM64, ARM64 QEMU and
  the Go 1.21–1.27 matrix. SSA performance conclusions apply to ARM64;
  Windows CI validates compatibility of the unchanged amd64 backend.

A follow-up guard (`36454ac`) preserves an opaque zero/trapping instruction instead of
mistaking it for an optimizer-created no-op. The benchmarked generators do
not emit that instruction; the measured revision remains explicitly recorded
as `6c7194b` so the data is reproducible. Its follow-up validation is tracked
in [this CI run](https://github.com/nyan233/jjs-go/actions/runs/45165970303).

## Reproduce the same-source control

Build both binaries before timing; use the same environment and alternate
which binary goes first:

```sh
go test -c -o /tmp/jjs-ssa-core.test ./core/compiler
go test -tags jitnossa -c -o /tmp/jjs-direct-core.test ./core/compiler
GOMAXPROCS=10 /tmp/jjs-ssa-core.test -test.run '^$' \
  -test.bench '^BenchmarkJITCodegen$' -test.benchmem -test.benchtime=300ms -test.cpu=10
GOMAXPROCS=10 /tmp/jjs-direct-core.test -test.run '^$' \
  -test.bench '^BenchmarkJITCodegen$' -test.benchmem -test.benchtime=300ms -test.cpu=10

cd benchmark/compare
go test -c -o /tmp/jjs-ssa-compare.test .
go test -tags jitnossa -c -o /tmp/jjs-direct-compare.test .
SONIC_NO_ASYNC_GC=1 GOMAXPROCS=10 /tmp/jjs-ssa-compare.test -test.run '^$' \
  -test.bench '^BenchmarkEncoder_(Binding|Generic|UnorderedGeneric)$/Large/jjs-go$' \
  -test.benchmem -test.benchtime=500ms -test.cpu=10
# Use fixed iterations for compilation: executable modules are retained.
GOMAXPROCS=10 /tmp/jjs-ssa-compare.test -test.run '^$' \
  -test.bench '^BenchmarkSSACompileFixture$' -test.benchmem -test.benchtime=50x -test.cpu=10
```
