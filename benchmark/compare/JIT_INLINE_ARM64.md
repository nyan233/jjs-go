# Typed JIT inlining and ARM64 Base64

Measured 2026-11-23 on Apple M1 Pro (10 cores, 32 GiB), macOS 27.0.1,
Go 1.25.0 darwin/arm64, AC power, `GOMAXPROCS=10`, `SONIC_NO_ASYNC_GC=1`.
Baseline: `ca8e881`; implementation: `c03d162` on `codex/jit-inline-primitives`.
SSA remains on its separate experimental branch.

## Implementation

- Bool and null literals are emitted directly. Integer width/sign loads are
  selected at compile time, followed by native unsigned division by 100,
  two-digit table lookup and bounded word copies. The reciprocal division
  first shifts by two so it remains exact through `MaxUint64`.
- Pointer nil tests and common slice loops are emitted into the caller.
  Saved/current roots live in initialized, GC-described stack slots, so Go
  allocation, stack growth and callbacks can safely occur inside an inline
  body. The JIT writes no new heap pointer fields without a Go write barrier.
- Capacity checks are inline. Buffer growth remains in the Go helper.
- Float loads, float32 conversion, finite/format selection and exponent
  cleanup are generated directly. Correctly rounded decimal conversion
  calls the shared `strconv.AppendFloat` implementation; its large conversion
  algorithm is intentionally not duplicated at each field. Non-finite values
  retain a cold error helper. Float32 format boundaries now match encoding/json.
- ARM64 byte slices use a hand-written `LD3`/bit-reshuffle/`TBL`/`ST4` NEON
  kernel: 48 input bytes become 64 output bytes. Scalar tails implement
  padding without overreading. `go generate ./core/compiler` extracts the
  relocatable machine-code body from the assembly, so the shared assembly
  entry and JIT inline entry use exactly the same kernel.
- The byte-slice nil check, output size, capacity check and quotes are inline
  for at most 49,152 input bytes. Larger values use the shared kernel in
  bounded chunks with Go scheduling points. The helper writes directly into
  reserved capacity, avoiding the previous zero-filled append. amd64 retains
  the standard Base64 transform with this improved reservation path.
- Inlining has a four-level nesting limit, recursive-type detection and a
  type-cost budget. Large arrays of eligible elements use loops rather than
  unrolling. Complex/recursive containers retain helper fallbacks. Primitive
  slice loops yield every 16,384 elements. Dynamic interfaces and custom
  marshalers preserve their dispatch behavior.

The algorithm research and primary-source links are in
[BASE64_ARM64_RESEARCH.md](BASE64_ARM64_RESEARCH.md).

## Measurement method

All timing used prebuilt binaries, run sequentially with no simultaneous
build/test/profile workload. This is normal desktop use, not an isolated
machine. Each steady-state row is the median of five 300 ms samples.
Microbenchmarks reuse output capacity and exercise the complete compiled
encoder entry; comparison benchmarks use the existing allocation-returning
Marshal APIs. JIT/type warmup is outside the steady-state timer.

Uncached compilation microbenchmarks use five samples of 50 compilations;
reflection/type metadata can be warm, but JIT code generation and loading
are performed anew. Fixture compilation uses five batches of 50 fresh
`EncoderCodegenStats` calls. These are not process-startup measurements.

All 460 primary benchmark samples are retained in
[jit-inline-arm64-before.txt](jit-inline-arm64-before.txt) and
[jit-inline-arm64-after.txt](jit-inline-arm64-after.txt), including the
additional fixture compilation measurements. Environment and revision details
are in [jit-inline-arm64-metadata.json](jit-inline-arm64-metadata.json).
Small differences, particularly in Generic, should be treated as noise.

## Reused-buffer microbenchmarks

| Case | Before ns/op | After ns/op | Time change | Before code B | After code B |
|---|---:|---:|---:|---:|---:|
| Bool | 17.1 | 16.37 | -4.3% | 80 | 208 |
| IntSmall | 18.27 | 16.56 | -9.4% | 96 | 384 |
| IntLarge | 34.34 | 27.47 | -20.0% | 96 | 384 |
| Record | 113.4 | 84.02 | -25.9% | 480 | 1664 |
| Pointer | 126 | 85.12 | -32.4% | 672 | 1984 |
| Ints | 171 | 84.36 | -50.7% | 192 | 880 |
| Records | 12890 | 9016 | -30.1% | 576 | 2144 |
| Bytes0 | 19.33 | 17.63 | -8.8% | 80 | 640 |
| Bytes12 | 27.94 | 24.97 | -10.6% | 80 | 640 |
| Bytes48 | 46.47 | 20.22 | -56.5% | 80 | 640 |
| Bytes1024 | 571.9 | 75.12 | -86.9% | 80 | 640 |
| Bytes65536 | 36043 | 4047 | -88.8% | 80 | 640 |

All microbenchmark cases remain at zero allocations/op with reused output.
The large-integer case encodes a negative 19-digit magnitude. `Ints` contains
11 mixed-size int64 values including both extremes. `Records` has 128 records.
The 1 KiB byte-slice case improves 7.61x and the 64 KiB case improves 8.91x;
these ratios include JSON wrapping and the encoder entry, not just the kernel.

## Existing comparison fixtures

| Family / size / library | Before ns/op | After ns/op | Time change | After allocs/op |
|---|---:|---:|---:|---:|
| Binding/Small/goccy | 560.5 | 593.8 | +5.9% | 2 |
| Binding/Small/sonic | 1599 | 1601 | +0.1% | 3 |
| Binding/Small/jjs-go | 639.5 | 517.3 | -19.1% | 2 |
| Binding/Medium/goccy | 4212 | 4084 | -3.0% | 2 |
| Binding/Medium/sonic | 16225 | 16345 | +0.7% | 3 |
| Binding/Medium/jjs-go | 5536 | 3801 | -31.3% | 2 |
| Binding/Large/goccy | 214709 | 213503 | -0.6% | 2 |
| Binding/Large/sonic | 483320 | 479295 | -0.8% | 107 |
| Binding/Large/jjs-go | 181749 | 159697 | -12.1% | 210 |
| Generic/Small/goccy | 1670 | 1703 | +2.0% | 1 |
| Generic/Small/sonic | 4283 | 4304 | +0.5% | 7 |
| Generic/Small/jjs-go | 1855 | 1814 | -2.2% | 11 |
| Generic/Medium/goccy | 25655 | 25931 | +1.1% | 1 |
| Generic/Medium/sonic | 51389 | 51071 | -0.6% | 40 |
| Generic/Medium/jjs-go | 19817 | 19336 | -2.4% | 77 |
| Generic/Large/goccy | 1.311e+06 | 1.35733e+06 | +3.5% | 1 |
| Generic/Large/sonic | 2.28472e+06 | 2.28258e+06 | -0.1% | 1268 |
| Generic/Large/jjs-go | 920210 | 916974 | -0.4% | 2529 |
| UnorderedGeneric/Small/goccy | 1445 | 1466 | +1.5% | 1 |
| UnorderedGeneric/Small/sonic | 3981 | 4047 | +1.7% | 7 |
| UnorderedGeneric/Small/jjs-go | 1903 | 1833 | -3.7% | 11 |
| UnorderedGeneric/Medium/goccy | 15653 | 15263 | -2.5% | 1 |
| UnorderedGeneric/Medium/sonic | 42620 | 42821 | +0.5% | 40 |
| UnorderedGeneric/Medium/jjs-go | 20348 | 20066 | -1.4% | 77 |
| UnorderedGeneric/Large/goccy | 865346 | 849615 | -1.8% | 1 |
| UnorderedGeneric/Large/sonic | 1.91662e+06 | 1.89831e+06 | -1.0% | 1267 |
| UnorderedGeneric/Large/jjs-go | 948222 | 912178 | -3.8% | 2529 |

Binding benefits substantially; Generic remains dominated by interface and
map handling. The map iterator allocations discussed in the earlier analysis
are unchanged. Ordinary Generic does not normalize key sorting across codecs;
UnorderedGeneric is the appropriate unsorted-map comparison. goccy and Sonic
are unchanged controls, and their variation is also retained above.

## Alternating Binding recheck

A separate recheck alternated baseline and new binaries for five rounds of
500 ms per case. All 30 samples are retained in
[jit-inline-arm64-recheck.txt](jit-inline-arm64-recheck.txt).

| Size | Before ns/op | After ns/op | Time change |
|---|---:|---:|---:|
| Small | 654.3 | 507.5 | -22.4% |
| Medium | 5382 | 3903 | -27.5% |
| Large | 184363 | 163424 | -11.4% |

One new-Large sample was 223,376 ns/op, above its other four samples
(157,420–176,811 ns/op). It remains included in the median; none were discarded.
The repeated comparison supports the direction of the Binding improvement,
while the exact percentage remains sensitive to desktop scheduling/noise.

## Code size and uncached compilation

| Microbenchmark type | Before compile ns/op | After compile ns/op | Change |
|---|---:|---:|---:|
| Bool | 6353 | 7122 | +12.1% |
| IntSmall | 6527 | 8856 | +35.7% |
| IntLarge | 6826 | 8883 | +30.1% |
| Record | 10344 | 18588 | +79.7% |
| Pointer | 17564 | 21450 | +22.1% |
| Ints | 13552 | 13218 | -2.5% |
| Records | 16182 | 23069 | +42.6% |

| Fixture type | Before code B | After code B | Functions before → after | Compile before → after, median |
|---|---:|---:|---:|---:|
| Book | 2,080 | 7,728 | 6 → 1 | 73.154 → 88.263 µs |
| TwitterResponse | 9,264 | 21,152 | 6 → 2 | 130.725 → 239.427 µs |

Inlining has a real size/compile cost: Twitter code grows 2.28x and uncached
compilation 1.83x; Book code grows 3.72x with a 1.21x compilation cost. This is
materially smaller than the earlier SSA experiment's roughly 7x cold penalty,
but it is not free. The implementation uses bounded expansion and shared
numeric conversion instead of reintroducing SSA. A future pass can merge
capacity checks or share cold growth blocks before a broader IR redesign.

## Validation

- Native Go 1.25 and Go 1.22.12 root tests/builds; comparison-module tests.
- Boundary and randomized differential tests against encoding/json/base64:
  integer widths/extremes, decimal powers, float32/64 boundaries, signed zero,
  NaN/Inf, nested pointers/slices/arrays, nil/empty values, custom marshaler
  errors, output growth, Base64 alignment/vector/chunk boundaries.
- Guard-page tests check exact source/destination bounds around vector/scalar
  tails. Shared and inlined kernels are exercised with the same source ends.
- Forced helper spills, fresh-goroutine stack growth and GC inside nested
  inline bodies, including the seven-word mixed integer/float ABI call.
- A regression test replaces generic type helpers with a panic entry to
  verify that the eligible typed fast paths no longer call them. Array-size
  regression checks ensure a longer array does not unroll a larger body.
- A 20-second fuzz run completed 1,745,936 executions without a failure.
- Code revision `c03d162` passed all CI jobs, including Windows amd64 Go 1.22
  and 1.25, Linux amd64, macOS ARM64, ARM64 QEMU and Go 1.21–1.27 compiler tests:
  [run 45154476413](https://github.com/nyan233/jjs-go/actions/runs/45154476413).

A separate existing issue was reproduced on unmodified `ca8e881` with native
Go 1.26.0: root test `TestJITFieldsAndOmitEmptyMatchEncodingJSON` crashes on a
single-pointer wrapper value. This also occurs on this branch; it is not fixed
by this optimization. The Go 1.26 compiler-package tests pass. No PMU cache-miss
or branch-misprediction measurements were collected in this study.

## Reproduce

Copy `core/compiler/jit_inline_benchmark_test.go` from this revision into the
baseline checkout before building its compiler test binary. It only uses APIs
already present on the baseline. Build both revisions before timing:

```sh
go test -c -o /tmp/compiler-inline.test ./core/compiler
GOMAXPROCS=10 /tmp/compiler-inline.test -test.run '^$' \
  -test.bench '^BenchmarkJITInline$' -test.benchmem \
  -test.benchtime=300ms -test.count=5 -test.cpu=10
GOMAXPROCS=10 /tmp/compiler-inline.test -test.run '^$' \
  -test.bench '^BenchmarkJITInlineCompile$' -test.benchmem \
  -test.benchtime=50x -test.count=5 -test.cpu=10

cd benchmark/compare
go test -c -o /tmp/compare-inline.test .
SONIC_NO_ASYNC_GC=1 GOMAXPROCS=10 /tmp/compare-inline.test -test.run '^$' \
  -test.bench '^BenchmarkEncoder_(Binding|Generic|UnorderedGeneric)$/(Small|Medium|Large)/(jjs-go|goccy|sonic)$' \
  -test.benchmem -test.benchtime=300ms -test.count=5 -test.cpu=10
```
