# Faster ARM64 appendQuoted on Apple M1 Pro

The encoder now uses a hand-written ARM64 routine that classifies, copies,
escapes and validates strings in one pass. Go retains ownership of allocation
and buffer growth. On the measured M1 Pro, the main string microbenchmarks
reduce time by 25–72% (8-byte ASCII is unchanged); whole-document encoding
improves by approximately 4–15% in the six existing Binding/Generic cases.
No SSA change is included. The baseline is `d22c43d`, implementation `c586c7e`,
and test-compatibility follow-up `7775c3e`.

## Algorithm and hardware instructions

The previous implementation called a scanner, later copied the scanned bytes,
and handled special characters and `utf8.DecodeRuneInString` in Go. The new
routine combines these steps:

1. Load 16 bytes with NEON, or 8 for short tails. Split each byte into low/high
   nibbles with `AND` and `USHR`.
2. Use two `TBL` lookups to classify all lanes. Intersect their class bits to
   identify control bytes, quote, backslash, HTML delimiters or non-ASCII.
   `UADDLV` reduces the flags without overflow; a zero sum means the entire
   vector is safe and can be stored directly from the loaded register.
3. Process a bounded scalar window when a special byte appears, avoiding a new
   SIMD scan at every escape. A 1 KiB packed table holds 2/6-byte escape outputs
   and their lengths; ordinary entries are zero. Stores write exactly the
   selected length, with no speculative output overwrite.
4. Validate UTF-8 with scalar ARM64 range checks and copy valid 2/3/4-byte
   sequences without reconstructing a Unicode code point. Reject overlong
   forms, surrogates, out-of-range and truncated sequences. Invalid bytes
   produce `\ufffd` one byte at a time, and U+2028/U+2029 stay escaped.

The vector classification tables total 32 declared bytes. The high-nibble
classes distinguish controls, quote/ampersand, angle brackets, backslash and
non-ASCII; low-nibble entries select the matching characters. This replaces a
chain of equality comparisons and OR operations with two table lookups.

The routine never reads past the input or writes past output capacity. When
space is insufficient it returns whole-unit input/output progress so Go can
grow the slice. A call processes at most 64 KiB plus one complete UTF-8 rune,
then returns to preemptible Go code. The compiled assembly text is 832 bytes
in the measured Go 1.25 binary. No cgo or Clang dependency is needed at runtime.
The amd64 implementation retains its existing Go/SSE/AVX path.

The approach was informed by [Sonic's bounded scan/copy quote loop](https://github.com/bytedance/sonic/blob/main/native/quote.c)
and [RapidJSON's SIMD string writer](https://github.com/Tencent/rapidjson/blob/master/include/rapidjson/writer.h).
The nibble-class tables here were constructed for jjs-go's HTML/UTF-8 semantics;
they are not a copy of either implementation. Instruction behavior was checked
against [Arm's NEON intrinsics reference](https://arm-software.github.io/acle/neon_intrinsics/advsimd.html).
[simdutf](https://simdutf.github.io/simdutf/index.html) demonstrates SIMD Unicode
validation, but this implementation uses a scalar UTF-8 path rather than
introducing a separate whole-string validation pass.

## What Clang -O3 showed

Apple Clang 21.0.0, `-O3 -mcpu=apple-m1`, did not automatically vectorize the
ordinary C loop that stops at the first special character. Its analysis remarks
identify an unsupported switch and early-exit control-flow shape. It did generate
NEON for explicitly written intrinsics. The two-table kernel uses `TBL`,
`USHR`, `AND`, `UMAXV` and vector load/store instructions; the Go assembly uses
widening `UADDLV` for reduction because its small class-bit sums fit in 16 bits.

Five standalone C runs, medians in ns/call, all-safe ASCII:

| Input | Scalar C | NEON comparisons | NEON table lookup |
| --- | ---: | ---: | ---: |
| 8 bytes | 5.715 | 7.555 | 7.545 |
| 32 bytes | 20.941 | 3.851 | 3.509 |
| 64 bytes | 41.271 | 6.461 | 4.738 |
| 256 bytes | 169.492 | 21.637 | 11.717 |
| 1,024 bytes | 657.399 | 81.685 | 42.015 |
| 4,096 bytes | 2,604.284 | 329.688 | 169.903 |

The table kernel takes about half the comparison kernel's time for long ASCII.
These C functions only scan/copy a safe prefix; they do not escape or validate
UTF-8, and their timings cannot be compared directly to `appendQuoted`.
See the [C source](experiments/quote-arm64/scan_copy.c),
[Clang assembly](experiments/quote-arm64/clang-o3.s.txt),
[vectorization remarks](experiments/quote-arm64/clang-vectorize.txt) and
[reproduction notes](experiments/quote-arm64/README.md).

## Measurement method

Native darwin/arm64, Apple M1 Pro, macOS 27.0.1, Go 1.25.0, on 2026-11-23.
Five baseline/optimized pairs alternate process order. Binaries were built
before timing. Each quote/hotpath sample runs for 200 ms; each document sample
for 500 ms. `GOMAXPROCS=10`, `-cpu=10`, `SONIC_NO_ASYNC_GC=1`. All suites run
sequentially, without overlapping builds, tests, fuzzing or profiling. Normal
desktop activity is not isolated. No samples were removed.

`Reuse` preallocates enough output; `Allocate` starts with nil. Whole-document
benchmarks warm type compilation before timing. The same new microbenchmark
source was compiled against the unchanged baseline before implementation.

## appendQuoted with buffer reuse

| Case | Before ns/op | After ns/op | Time change | Faster pairs |
| --- | ---: | ---: | ---: | ---: |
| Unicode | 1,088.00 | 637.00 | -41.5% | 5/5 |
| UnicodeThenASCII | 108.50 | 68.97 | -36.4% | 5/5 |
| Emoji | 583.70 | 354.60 | -39.2% | 5/5 |
| Escapes | 832.80 | 413.50 | -50.3% | 5/5 |
| HTML | 2,268.00 | 640.40 | -71.8% | 5/5 |
| Controls | 2,141.00 | 923.60 | -56.9% | 5/5 |
| InvalidUTF8 | 1,672.00 | 608.60 | -63.6% | 5/5 |
| LateEscape | 114.40 | 56.61 | -50.5% | 5/5 |
| ASCII0 | 3.17 | 2.08 | -34.2% | 5/5 |
| ASCII4 | 11.43 | 8.54 | -25.3% | 5/5 |
| ASCII8 | 7.28 | 7.29 | +0.2% | 2/5 |
| ASCII16 | 9.28 | 6.64 | -28.4% | 5/5 |
| ASCII32 | 10.76 | 7.59 | -29.5% | 5/5 |
| ASCII64 | 13.65 | 10.15 | -25.6% | 5/5 |
| ASCII256 | 31.00 | 18.06 | -41.7% | 5/5 |
| ASCII1024 | 101.40 | 54.26 | -46.5% | 5/5 |

All these reuse cases remain at 0 B/op and 0 allocs/op. The representative
non-ASCII/escape cases improve in all five pairs. The first prototype regressed
8-byte ASCII; adding an 8-lane NEON path restored parity. Empty and shorter
strings use small scalar paths.

The largest microbenchmark span `(max-min)/median` is 67.5% for the baseline
UnicodeThenASCII case; its optimized samples still win every paired round.
Most cases have much smaller spans. Treat sub-percent differences such as
8-byte ASCII as unchanged.

Starting with nil also reduces allocation count in most nontrivial cases:

| Case | Allocs/op before → after | B/op before → after |
| --- | ---: | ---: |
| ASCII 1,024 | 2 → 1 | 1,160 → 1,152 |
| Unicode | 2 → 1 | 904 → 896 |
| Mixed escapes | 7 → 2 | 1,016 → 992 |
| HTML escapes | 11 → 4 | 8,440 → 7,104 |
| Controls | 10 → 3 | 5,368 → 4,032 |
| Invalid UTF-8 | 10 → 4 | 5,368 → 6,048 |

Invalid UTF-8 illustrates the growth tradeoff: fewer growth operations can still
allocate more total bytes because capacity rounds differently. The wrapper
reserves ordinary output size first, rather than allocating six times every
input. Exact sufficient buffers are reused, including across chunk boundaries.

## Whole-document encoding

| Case | Before ns/op | After ns/op | Time change | Faster pairs |
| --- | ---: | ---: | ---: | ---: |
| Binding / Small | 692.00 | 657.00 | -5.1% | 4/5 |
| Binding / Medium | 5,927.00 | 5,387.00 | -9.1% | 5/5 |
| Binding / Large | 255,932.00 | 217,095.00 | -15.2% | 4/5 |
| Generic / Small | 1,959.00 | 1,854.00 | -5.4% | 5/5 |
| Generic / Medium | 20,697.00 | 19,831.00 | -4.2% | 5/5 |
| Generic / Large | 1,075,666.00 | 982,508.00 | -8.7% | 5/5 |
| UnorderedGeneric / Small | 1,946.00 | 1,885.00 | -3.1% | 5/5 |
| UnorderedGeneric / Medium | 20,475.00 | 19,968.00 | -2.5% | 5/5 |
| UnorderedGeneric / Large | 1,073,234.00 | 981,447.00 | -8.6% | 5/5 |

Allocation counts remain unchanged: Binding Small/Medium/Large use 2/2/210;
Generic and UnorderedGeneric use 11/77/2,529. Small B/op differences in large
cases come from amortized runtime/pool effects, not a new per-field allocation.

Binding Large had a 52.4% span in the first optimized series, so it received a
separate five-pair, one-second check: median **261,346 → 207,360 ns/op (-20.7%)**,
with all five optimized rounds faster. Baseline/optimized spans in that check
were 6.8%/12.6%. Both series remain in the raw output; do not treat either
percentage as a universal speedup.

The public string-encoding hotpaths reduce time by 15.3% for long ASCII, 37.0%
for Unicode and 47.6% for mixed escapes. Buffer-reuse and dynamic-field hotpaths
improve too. Decode-only control measurements move between about -1.0% and
+2.3%; decoding code was not changed.

## Library comparison after the change

Three 300 ms samples per case, median ns/op. goccy v0.11.0 and Sonic v1.15.5,
with the same compiler and CPU configuration. Binding uses the existing
repository configurations. UnorderedGeneric disables map sorting for every
library and keeps HTML escaping and string validation enabled where applicable.

| Case | jjs-go ns/op | goccy ns/op | Sonic ns/op |
| --- | ---: | ---: | ---: |
| Binding / Small | 676.5 | 625.0 | 1,617.0 |
| Binding / Medium | 5,650.0 | 4,086.0 | 16,414.0 |
| Binding / Large | 224,404.0 | 232,350.0 | 496,579.0 |
| UnorderedGeneric / Small | 1,892.0 | 1,487.0 | 4,101.0 |
| UnorderedGeneric / Medium | 20,918.0 | 15,545.0 | 42,731.0 |
| UnorderedGeneric / Large | 985,669.0 | 891,449.0 | 1,885,293.0 |

jjs-go remains slower than goccy on Small/Medium and on unordered Generic.
Binding Large is close in these samples; a small median lead is not a robust
ranking given desktop noise. These results are specific to this ARM64 machine
and do not replace the historical Windows amd64 README tables.

## Profile and validation

The original profile exposed `appendQuoted`, `utf8.DecodeRuneInString`, the
scanner and copying work. After the change, string processing samples are in
`quoteJSONString`; the measured string path no longer calls the Go UTF-8 rune
decoder or the separate encoder scanner. Allocation/GC remains visible.

Both Darwin CPU profiles attribute roughly 77–78% of samples to
`runtime.kevent`, so raw profile percentages are not reliable CPU-utilization
or hardware-counter evidence. Timing measurements above are primary; profiles
only corroborate the changed call path. No cache-miss or branch-misprediction
rate is claimed. The raw report includes both profile summaries.

Validation includes native full tests, race tests, comparison-module tests,
all byte values at multiple offsets, source/destination alignment and capacity
combinations, protected source/destination pages, malformed UTF-8 and 64 KiB
chunk boundaries. Exact escape goldens and comparison with the old Go routine
preserve output bytes; independent standard-library decoding checks preserve
semantics across Go versions whose equivalent escape spelling differs.

The final 10-second `FuzzAppendQuoted` run passed **302,016 executions**; earlier
implementation runs also passed. [All 15 CI jobs for 7775c3e passed](https://github.com/nyan233/jjs-go/actions/runs/45132938958),
including Windows amd64 Go 1.22/1.25, macOS, ARM64 cross-build/assembly and QEMU,
and the Go 1.21–1.27 compiler/loader matrix.

## Reproduce

```sh
# From the repository root. Build binaries first, then alternate revisions.
go test -c -o /tmp/quote-compiler.test ./core/compiler
GOMAXPROCS=10 /tmp/quote-compiler.test -test.run='^$' \
  -test.bench='^BenchmarkAppendQuoted$' -test.benchmem \
  -test.benchtime=200ms -test.count=1 -test.cpu=10

cd benchmark/compare
go test -c -o /tmp/quote-compare.test .
GOMAXPROCS=10 SONIC_NO_ASYNC_GC=1 /tmp/quote-compare.test -test.run='^$' \
  -test.bench='^BenchmarkEncoder_(Binding|Generic|UnorderedGeneric)$/(Small|Medium|Large)/jjs-go$' \
  -test.benchmem -test.benchtime=500ms -test.count=1 -test.cpu=10
```

[Complete raw measurements](results-darwin-arm64-append-quoted.txt) include the
five main pairs, the large-document check, library comparison, Clang probe,
profile summaries and the exploratory versions.
