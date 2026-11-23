# Hot-path optimization measurements

Measured on 2026-11-23 against correctness-fix commit `5b211eb7c265a02db819aa123b12091e5287b3c6`.
The after results correspond to `codex/optimize-codec-hotpaths` with this report.

## Environment and method

- Apple M1 Pro, 10 CPU cores, 32 GiB RAM, macOS 27.0.1 (26A434).
- Go 1.25.0, darwin/arm64. `-cpu=1`, except the cache contention test (`-cpu=8`).
- Both binaries were built with the same toolchain. The baseline used an isolated checkout of `5b211eb` with only the new benchmark source files copied in.
- Five samples per case; before/after binaries ran sequentially, reversing their order on alternate rounds. No builds or other test suites ran alongside these final measurements.
- Codec samples used 300 ms per case; microbenchmarks used 200 ms; parallel cache and `MarshalTo` used 300 ms. Compilation/type caches were warmed outside timers.
- Tables show medians and elapsed-time changes (`after / before - 1`); negative is faster. These are local warm-cache measurements, not cold-start or Windows amd64 measurements.
- The unchanged stdlib controls moved between -4.2% and +2.5% across codec cases. Small changes of a few percent should not be treated as statistically established gains.

## Changes measured together

1. Resume bulk ASCII scans after UTF-8; copy contiguous safe string spans together. Avoid rescanning validated decode prefixes and validating UTF-8 twice. Back off scanning on dense escapes.
2. Keep required struct fields on the static encoding path even when sibling fields are optional. Merge adjacent JSON literals into one reserve/copy operation.
3. Fast-path exact built-in interface types; share compiled programs and reuse the current encoding context across nested values while preserving GC-visible parent state.
4. Bound learned capacity hints at 1 MiB, allow hints below the initial estimate, avoid unchanged atomic writes, and respect caller-owned capacity. Add `MarshalTo`, and pass output slices by value internally to remove a temporary header allocation.
5. Match the next expected struct field directly, use exact-name indexes for wide structs, and parse typed integers with validation and overflow checks in one pass.
6. Publish immutable type-cache snapshots for lock-free hits, deduplicate each cold type independently, and share root/dynamic plans. Runtime module registration remains serialized. Publishing a new snapshot copies the existing map; this favors repeated use of a bounded type set over generating unbounded new types.

## Full codec fixtures

Binding uses concrete Go structs; Generic uses `any` values. Binding decode reuses the destination; FreshBinding creates a zero-value destination on every iteration. Generic decode resets its destination to nil each time.

| jjs-go case | Before µs/op | After µs/op | Time change | B/op before → after | allocs/op before → after |
| --- | ---: | ---: | ---: | ---: | ---: |
| Encoder Binding/Small | 1.511 | 0.909 | -39.8% | 2,040 → 608 | 3 → 2 |
| Encoder Binding/Medium | 14.355 | 8.202 | -42.9% | 28,074 → 9,616 | 4 → 2 |
| Encoder Binding/Large | 536.311 | 340.582 | -36.5% | 798,424 → 282,264 | 212 → 210 |
| Decoder Binding/Small | 1.686 | 1.611 | -4.4% | 64 → 64 | 7 → 7 |
| Decoder Binding/Medium | 33.488 | 31.496 | -5.9% | 3,272 → 3,272 | 107 → 107 |
| Decoder Binding/Large | 1,658.107 | 1,577.143 | -4.9% | 203,369 → 203,350 | 4236 → 4236 |
| Decoder FreshBinding/Small | 2.315 | 2.131 | -7.9% | 544 → 544 | 13 → 13 |
| Decoder FreshBinding/Medium | 38.290 | 34.908 | -8.8% | 8,664 → 8,664 | 125 → 125 |
| Decoder FreshBinding/Large | 1,758.148 | 1,691.722 | -3.8% | 436,984 → 436,984 | 4379 → 4379 |
| Encoder Generic/Small | 4.816 | 2.877 | -40.3% | 1,368 → 1,344 | 12 → 11 |
| Encoder Generic/Medium | 47.322 | 30.842 | -34.8% | 16,794 → 16,768 | 78 → 77 |
| Encoder Generic/Large | 2,146.326 | 1,502.955 | -30.0% | 717,937 → 717,841 | 2530 → 2529 |
| Decoder Generic/Small | 4.915 | 4.651 | -5.4% | 3,592 → 3,592 | 72 → 72 |
| Decoder Generic/Medium | 75.989 | 73.418 | -3.4% | 51,544 → 51,544 | 747 → 747 |
| Decoder Generic/Large | 3,778.153 | 3,416.606 | -9.6% | 2,132,657 → 2,132,656 | 30873 → 30873 |

Typed encoding elapsed time fell 36.5–42.9%; generic encoding fell 30.0–40.3%.
Typed encoding allocation bytes fell about 65–70%. Decoder allocation counts stayed unchanged.

## Focused workloads

| Case | Before ns/op | After ns/op | Time change | B/op before → after | allocs/op before → after |
| --- | ---: | ---: | ---: | ---: | ---: |
| Strings/ASCII/Encode | 521.400 | 481.100 | -7.7% | 1,192 → 1,168 | 3 → 2 |
| Strings/ASCII/Decode | 333.900 | 341.100 | +2.2% | 1,024 → 1,024 | 1 → 1 |
| Strings/UnicodeThenASCII/Encode | 3,244.000 | 505.900 | -84.4% | 1,192 → 1,168 | 3 → 2 |
| Strings/UnicodeThenASCII/Decode | 1,231.000 | 372.200 | -69.8% | 1,152 → 1,152 | 1 → 1 |
| Strings/Unicode/Encode | 1,816.000 | 1,328.000 | -26.9% | 936 → 912 | 3 → 2 |
| Strings/Unicode/Decode | 2,023.000 | 1,254.000 | -38.0% | 768 → 768 | 1 → 1 |
| Strings/LateEscape/Encode | 520.900 | 489.600 | -6.0% | 1,192 → 1,168 | 3 → 2 |
| Strings/LateEscape/Decode | 2,058.000 | 488.400 | -76.3% | 3,456 → 2,432 | 3 → 2 |
| Strings/Escapes/Encode | 1,007.000 | 969.100 | -3.8% | 488 → 464 | 3 → 2 |
| Strings/Escapes/Decode | 814.400 | 838.000 | +2.9% | 320 → 320 | 1 → 1 |
| DynamicFields | 305.300 | 137.000 | -55.1% | 280 → 80 | 2 → 1 |
| Interfaces | 902.100 | 451.800 | -49.9% | 496 → 312 | 5 → 4 |
| WideDecode | 3,566.000 | 1,490.000 | -58.2% | 0 → 0 | 0 → 0 |
| Integers | 563.100 | 381.200 | -32.3% | 0 → 0 | 0 → 0 |
| BufferReuse | 123.700 | 76.530 | -38.1% | 0 → 0 | 0 → 0 |
| ParallelCache | 14.240 | 10.740 | -24.6% | 0 → 0 | 0 → 0 |
| MixedSizes | 26,631.000 | 26,745.000 | +0.4% | 251,330 → 221,384 | 2 → 1 |
| ParallelCache-8 | 123.500 | 1.472 | -98.8% | 0 → 0 | 0 → 0 |

The new public `MarshalTo` benchmark, encoding a pointer to a warmed small record into a reused buffer, measured **81.87 ns/op, 0 B/op, 0 allocs/op**. This is workload-specific; custom marshalers, maps and buffer growth can still allocate.

ASCII and dense-escape decoding were 2.2% and 2.9% slower in this run. MixedSizes was 0.4% slower while reducing allocation bytes by 11.9%; it alternates one 2 MB string record with 31 small records. These small regressions are retained in the table rather than claimed as improvements. ParallelCache-8 reports aggregate throughput across eight workers, not single-request latency.

## Reproduction

Build the baseline and candidate binaries first, then run them sequentially. Copy `hotpath_benchmark_test.go` and `benchmark/compare/benchmark_test.go` into the isolated baseline checkout before building, so both sides use identical workloads. The API-only `BenchmarkMarshalTo` lives in `optimization_test.go` and only runs on the candidate.

```sh
# Run inside each checkout's benchmark/compare directory:
go test -c -o /tmp/codec-VARIANT.test .
/tmp/codec-VARIANT.test -test.run '^$' \
  -test.bench 'Benchmark(Encoder|Decoder)_(Binding|Generic|FreshBinding)/(Small|Medium|Large)/(jjs-go|stdlib)$' \
  -test.benchmem -test.benchtime=300ms -test.count=1 -test.cpu=1

# Run inside each checkout's root:
go test -c -o /tmp/hotpaths-VARIANT.test .
/tmp/hotpaths-VARIANT.test -test.run '^$' -test.bench '^BenchmarkHotpath' \
  -test.benchmem -test.benchtime=200ms -test.count=1 -test.cpu=1
/tmp/hotpaths-VARIANT.test -test.run '^$' -test.bench '^BenchmarkHotpathParallelCache$' \
  -test.benchmem -test.benchtime=300ms -test.count=1 -test.cpu=8

# Candidate only:
go test -run '^$' -bench '^BenchmarkMarshalTo$' -benchmem -benchtime=300ms -count=5 -cpu=1
```

Repeat each before/after pair five times, reversing the order on alternate rounds. Raw samples include the exact binary invocation for each round:

- Codec: [before](results-arm64-hotpaths-codec-before.txt), [after](results-arm64-hotpaths-codec-after.txt), including stdlib controls.
- Microbenchmarks: [before](results-arm64-hotpaths-hotpaths-before.txt), [after](results-arm64-hotpaths-hotpaths-after.txt).
- Eight-worker cache: [before](results-arm64-hotpaths-parallel-before.txt), [after](results-arm64-hotpaths-parallel-after.txt).
- Public buffer reuse: [MarshalTo](results-arm64-hotpaths-marshalto.txt).

## Correctness coverage

Regression cases compare string escaping, invalid UTF-8, integer bounds, mixed optional fields, wide-field ordering and nested interfaces against `encoding/json`. They also cover buffer ownership/reuse, GC during nested encoding, concurrent cold type compilation, same-type compile deduplication and cached failures. Dynamic-type tests exposed a uintptr-to-pointer round trip; the decoder now retains the original runtime type pointer.
