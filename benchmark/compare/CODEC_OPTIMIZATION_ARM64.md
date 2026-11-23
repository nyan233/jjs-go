# Decoder optimization and encoder/SIMD study on M1 Pro

This follows [the decoder diagnosis](DECODER_ANALYSIS_ARM64.md). Measurements
use native darwin/arm64 on Apple M1 Pro, macOS 27.0.1, Go 1.25.0, on 2026-11-23.
The baseline is `029830e` / codec revision `96ffa5b`; optimized runtime is
`0781cbe`. Library versions remain goccy v0.11.0 and Sonic v1.15.5. The original README tables remain historical.

## Changes implemented

- Keep depth checks and deferred cleanup in small wrappers so Go can use
  open-coded defers. Reject excess nesting before incrementing the depth.
- Keep the no-whitespace path inlinable and scan whitespace runs with a local
  cursor. Avoid scanning a string's terminating byte twice.
- Validate skipped strings without unescaping or allocating them. Word scanning
  can pass over UTF-8 bytes directly because no replacement string is needed.
- Copy short strings into append-only chunks, batch interface string/float
  payloads in typed slices, and reuse bounded scratch through `sync.Pool`.
  Published data is never overwritten. Long strings get separate storage.
- Stage generic object entries/array elements, then allocate containers at
  their final size. Preserve duplicate-key order and non-nil empty arrays.
- Cache Unicode case-folded field names for wider structs, preserving exact
  matches and declaration-order precedence.
- Add ARM64 NEON encoder scanning after an eight-byte prefix check. Keep short
  input handling in the same Go function to avoid an extra helper call.
  Decoder scanning retains the word implementation after end-to-end testing.

The scratch object retains at most a 16 KiB current string chunk and a small
float slab. Scratch container capacities above 4,096 elements are discarded
from the pool. String boxes are detached on release so the pool cannot retain
prior result strings. Batched allocation can keep neighboring values alive
together; it is not a zero-copy API and does not borrow caller input.

Staging containers trades temporary entry storage for fewer map/slice growth
allocations. Objects with many duplicate keys can reserve more space than their
final number of distinct keys; the published cases do not measure that extreme.

## Before and after

Five 500 ms samples per case, with baseline/optimized process order alternating
between rounds. Binaries were built before measurement. Benchmarks are serial,
with `GOMAXPROCS=10`, `-cpu=10`, and `SONIC_NO_ASYNC_GC=1`. No builds, fuzzers or
other benchmark suites ran alongside the measured processes. The Mac remained
in normal desktop use; small percentage differences are not firm rankings.

| Case | Before ns/op | After ns/op | Time change | Allocs/op before → after |
| --- | ---: | ---: | ---: | ---: |
| Encoder / Binding / Small | 693.9 | 686.1 | -1.1% | 2 → 2 |
| Encoder / Binding / Medium | 6,128 | 5,878 | -4.1% | 2 → 2 |
| Encoder / Binding / Large | 266,460 | 256,848 | -3.6% | 210 → 210 |
| Decoder / Binding / Small | 1,427 | 901.8 | -36.8% | 7 → 0 |
| Decoder / Binding / Medium | 27,429 | 16,862 | -38.5% | 107 → 1 |
| Decoder / Binding / Large | 1,458,261 | 849,380 | -41.8% | 4,236 → 444 |
| Decoder / FreshBinding / Small | 1,641 | 1,094 | -33.3% | 13 → 6 |
| Decoder / FreshBinding / Medium | 29,459 | 18,501 | -37.2% | 125 → 19 |
| Decoder / FreshBinding / Large | 1,547,858 | 918,228 | -40.7% | 4,379 → 587 |
| Encoder / Generic / Small | 1,965 | 1,947 | -0.9% | 11 → 11 |
| Encoder / Generic / Medium | 20,939 | 20,503 | -2.1% | 77 → 77 |
| Encoder / Generic / Large | 1,093,283 | 1,070,830 | -2.1% | 2,529 → 2,529 |
| Decoder / Generic / Small | 3,586 | 2,638 | -26.4% | 72 → 23 |
| Decoder / Generic / Medium | 62,079 | 35,587 | -42.7% | 747 → 134 |
| Decoder / Generic / Large | 2,841,591 | 1,564,348 | -44.9% | 30,873 → 4,084 |

The largest within-case range `(max - min) / median` was 10.7%.
The 2–4% encoder median improvements are modest relative to some sample ranges;
Generic Medium/Large improved in all five paired rounds, while typed encoder
rounds were noisier. The much larger decoder improvements are consistent.
No samples were excluded.

Binding reuses its destination; FreshBinding starts from a zero value; Generic
starts from nil. Every library is warmed before timing. Compilation is excluded.
Reported zero allocations are amortized rounded counts, not an assertion that
no allocation ever occurs.

## Comparison after optimization

Three 500 ms samples per library/case, medians in ns/op:

**Decoder / Binding**

| Size | jjs-go | goccy | Sonic |
| --- | ---: | ---: | ---: |
| Small | 902.1 | 432.1 | 1,365 |
| Medium | 16,843 | 8,422 | 22,369 |
| Large | 849,795 | 438,461 | 903,636 |

**Decoder / Generic**

| Size | jjs-go | goccy | Sonic |
| --- | ---: | ---: | ---: |
| Small | 2,649 | 2,128 | 2,626 |
| Medium | 35,473 | 25,012 | 37,447 |
| Large | 1,566,459 | 1,101,339 | 1,571,793 |

**Decoder / FreshBinding**

| Size | jjs-go | goccy | Sonic |
| --- | ---: | ---: | ---: |
| Small | 1,106 | 725.1 | 1,648 |
| Medium | 18,755 | 10,420 | 24,130 |
| Large | 920,855 | 485,490 | 917,159 |

**Encoder / Binding**

| Size | jjs-go | goccy | Sonic |
| --- | ---: | ---: | ---: |
| Small | 688.1 | 566.2 | 1,575 |
| Medium | 5,877 | 4,020 | 16,398 |
| Large | 257,670 | 215,070 | 486,723 |

**Encoder / Generic**

| Size | jjs-go | goccy | Sonic |
| --- | ---: | ---: | ---: |
| Small | 1,955 | 1,690 | 4,253 |
| Medium | 20,450 | 25,138 | 50,862 |
| Large | 1,089,892 | 1,309,062 | 2,271,003 |

**Encoder / UnorderedGeneric**

| Size | jjs-go | goccy | Sonic |
| --- | ---: | ---: | ---: |
| Small | 1,908 | 1,435 | 4,038 |
| Medium | 19,835 | 15,235 | 43,365 |
| Large | 1,067,495 | 846,151 | 1,888,497 |

The unordered encoder benchmark changes only map ordering relative to the
existing comparison settings: goccy uses `UnorderedMap`, and Sonic uses the
`ConfigStd` fields with `SortMapKeys=false`. HTML escaping, UTF-8 validation and
custom marshaler behavior remain enabled. Differential tests verify output
values, including invalid UTF-8 and HTML-sensitive characters.

The existing default Generic encoder comparison includes key sorting in goccy
and Sonic, whereas jjs does not sort. The unordered result is the appropriate
comparison when evaluating dynamic encoding work with that difference removed.

## Encoder CPU and allocation profiles

Eight baseline cases were separately profiled for six seconds: jjs Binding
Small/Medium/Large, goccy and Sonic Binding Large, and all three Generic Large.
Optimized jjs Binding/Generic Large encoding and decoding were also profiled.

The following baseline percentages are filtered to benchmark caller stacks
with `-focus='benchmark(Binding|Generic)Encode' -relative_percentages`.
They exclude background runtime/GC stacks, are not wall-time percentages,
and cumulative rows overlap.

| Baseline case | Hot path | Focused sample share |
| --- | --- | ---: |
| jjs Binding / Small | `appendQuoted` | 18.9% cumulative |
| jjs Binding / Small | `strconv.ryuDigits` | 12.3% cumulative |
| jjs Binding / Medium | `appendQuoted` | 35.8% cumulative |
| jjs Binding / Medium | `jitReserveRaw` | 9.9% cumulative |
| jjs Binding / Large | `appendQuoted` | 59.1% cumulative |
| jjs Binding / Large | `utf8.DecodeRuneInString` | 19.0% flat, included above |
| jjs Generic / Large | `appendQuoted` | 43.9% cumulative |
| jjs Generic / Large | `runtime.mallocgc` | 14.5% cumulative |
| goccy Binding / Large | `AppendString` | 55.3% cumulative |
| goccy Generic / Large | `MapContext.SortKeys` | 32.8% cumulative |
| Sonic Binding / Large | encoder VM execution | 31.4% flat |
| Sonic Binding / Large | native NEON UTF-8 validation | 19.3% flat |
| Sonic Binding / Large | native NEON quote / HTML escape | 10.0% / 6.4% flat |

The most promising remaining encoder work is Unicode/string processing,
reducing frequent reserve-helper calls for static segments, and reducing
per-map iterator/context allocations. Small inputs also spend meaningful time
formatting floats; this is expected conversion work, not evidence of a cache bug.
Sonic's native string routines do not eliminate its ARM64 VM/interpreter cost.

After optimization, Binding Large encoding still spends 56.5% cumulatively in
`appendQuoted`, including 19.6% flat in UTF-8 rune decoding. Generic Large is
39.6% cumulatively in quoting. The NEON scanner itself accounts for 2.7% and
1.3% flat respectively, so SIMD scanning is only a small part of total encoding.

Heap profiles provide a particularly concrete next target: `jitEncodeMap`
accounts for 99.7% of sampled Generic Large allocation objects and 32.9% of
allocated bytes. `-benchmem` reports 2,529 allocations/op and about 718 KB/op,
versus goccy's one allocation and 475 KB/op. This points to the map iterator
bridge, not type-plan compilation. These sampled allocation shares are not
CPU-time shares.

For optimized decoding, Binding Large spends 35.1% cumulatively in string
parsing and 21.3% in whitespace handling; Generic Large spends 42.7% in string
parsing and 14.6% in map assignment. Runtime defer bookkeeping and repeated map
growth are no longer leading entries in the focused profiles. The remaining
decode work is primarily parsing/scanning and final value construction.

### Anomalous runtime samples

Unfiltered baseline profiles place many samples in `runtime.kevent`,
`pthread_cond_wait`, `pthread_kill` and `usleep`. For example, Binding Large
reports 14.84 seconds of samples over 9.87 seconds of wall time, with `kevent`
at 26.0% and `pthread_kill` at 14.7%. Parallel GC can legitimately make summed
CPU time exceed wall time, so that ratio alone does not prove a profiler bug.

Darwin CPU profiling has known reports of syscall bias/over-counting
([Go issue #68477](https://github.com/golang/go/issues/68477),
[#57722](https://github.com/golang/go/issues/57722)). This is a reason to be
cautious about the runtime-heavy samples here, not proof that these measurements
have exactly the same cause. Codec attribution uses focused profiles and
unprofiled throughput checks; no cache or lock-contention diagnosis is inferred
from the raw runtime percentages. A separate `GOMAXPROCS=1` profile is retained
as a diagnostic, not substituted into the throughput tables.

With `GOMAXPROCS=1`, the raw diagnostic still attributes 50.1% of samples to
`runtime.kevent`; reducing GC parallelism does not make the raw profile a clean
codec attribution. The hot codec routines remain string quoting and UTF-8
handling. This strengthens the case for using unprofiled benchmark deltas to
validate changes rather than interpreting raw syscall shares as application
bottlenecks.

## Cache misses and branch prediction

**Hardware miss rates were not measured.** `xcode-select -p` points to
`/Library/Developer/CommandLineTools`; `xctrace` reports that full Xcode is
required, and no Xcode installation was found by Spotlight. `kern.kpc` is not
an exposed sysctl on this host. No system files, developer-directory selection,
or security settings were changed.

CPU sampling identifies expensive code but cannot tell how often an L1/L2
access missed or a branch was mispredicted. Apple's
[CPU Counters workflow](https://developer.apple.com/documentation/xcode/addressing-cpu-bottlenecks)
is the appropriate next measurement when Instruments is available. Record
instructions/cycles, branch events/mispredictions, and L1D/L2 misses for the
same prebuilt binary and fixture, keeping performance and efficiency cores
separate where the tooling allows it.

Source/profile evidence identifies candidates, not measured miss rates:

- Generic maps, pointer-rich interface values and repeated allocation/growth
  create memory traffic. The changes reduce this work and allocation counts;
  an improvement in hardware cache-hit rate is not established.
- Per-byte scanners and recursive dispatch contain many branches. Word/NEON
  loops reduce checks per byte on long safe runs, but actual prediction quality
  depends on the input. Early-special-byte cases are measured separately.
- The type-plan cache is warmed, and cache lookup/compilation is not a leading
  codec hotspot in these samples. A software cache hit does not imply a CPU
  data-cache hit.

Repeated fixtures also produce warm code/data and regular branch patterns.
These results do not establish prediction or cache behavior for mixed production
inputs with different field order, string lengths or value types.

## ARM64 SIMD results and selection

The original ARM64 implementation is scalar 64-bit word scanning, not NEON.
The new assembly classifies 16 bytes using `VLD1`, `VCMEQ`, `VUSHR`, `VORR`,
`VAND` and `VUADDLV`, then locates the first special byte. It does not load
outside the provided length; a final overlapping vector avoids scalar tails.
This is a special-byte scanner, not a SIMD UTF-8 validator or unescaper.

Initial microbenchmarks used byte scanning, word scanning and direct NEON calls,
with three 100 ms samples and `GOMAXPROCS=1`. For 1,024 safe ASCII bytes, direct
NEON took about 84 ns versus 282 ns for encoder word scanning, and 54 ns versus
157 ns for decoder word scanning. However, a special/non-ASCII first byte made
the direct NEON call about 20–27% slower than the word scanner.

Directly dispatching every input of at least 16 bytes to NEON improved the
full encoder's Medium/Large cases by roughly 3–4%, but regressed typed decoding
by 4–6% and Generic Large by 4%. Decoder calls pass the remaining document
length, so short string matches frequently paid the SIMD setup cost.

An eight-byte prefix gate reduced the decoder regression but did not establish
a repeatable end-to-end decoder gain. Production decoding therefore retains
word scanning. Encoding uses the prefix gate and only invokes NEON after a safe
first word with at least 16 bytes remaining; dense-escape backoff is preserved.

Final scanner medians, ns/op. `Dispatch` is the production choice:

| Mode / input | Word | Direct NEON | Dispatch |
| --- | ---: | ---: | ---: |
| Encode / ASCII / 16 B | 5.10 | 3.29 | 5.45 |
| Encode / ASCII / 24 B | 7.20 | 4.63 | 5.39 |
| Encode / ASCII / 64 B | 17.72 | 7.17 | 9.13 |
| Encode / ASCII / 1024 B | 279.60 | 83.47 | 85.30 |
| Encode / EarlyEscape / 1024 B | 2.75 | 3.29 | 3.01 |
| Encode / Unicode / 1024 B | 2.75 | 3.29 | 3.01 |
| Decode / ASCII / 16 B | 4.75 | 2.71 | 5.39 |
| Decode / ASCII / 24 B | 6.87 | 3.81 | 7.51 |
| Decode / ASCII / 64 B | 11.74 | 5.08 | 12.40 |
| Decode / ASCII / 1024 B | 155.10 | 52.43 | 155.70 |
| Decode / EarlyEscape / 1024 B | 2.22 | 2.79 | 2.54 |
| Decode / Unicode / 1024 B | 2.22 | 2.78 | 2.53 |

These scanner numbers are not full JSON speedups. Unicode-heavy strings still
need rune validation/replacement and escaping; accelerating ASCII runs does
not remove that cost. The adopted whole-codec results are in the first table.

## Validation and artifacts

- Root and comparison-module tests; root race tests.
- New differential tests for skipped malformed strings, invalid UTF-8,
  unpaired surrogates, nesting cleanup, duplicate/nested containers and folded
  field names.
- Retained-result tests across input mutation, pooled storage reuse, GC and
  concurrent decodes; allocation regression for skipping escaped values.
- NEON tests cover alignments, special bytes, vector/tail boundaries, and
  inaccessible guard pages immediately after input.
- Differential fuzzing of Generic decode, string decode, string encode and
  skipped values; Windows amd64 cross-compilation and ARM64 fallback compilation.

Runtime commit `0781cbec31c371099f03bffc88608eb4d007db7b` passed all 15
[CI jobs](https://github.com/nyan233/jjs-go/actions/runs/45122762612), including
Windows amd64 Go 1.22/1.25, native macOS, Linux ARM64 QEMU and the Go 1.21–1.27
loader/compiler matrix.

Raw output and profile summaries are in
[the measurement record](results-darwin-arm64-codec-optimization.txt).
Full local profiles/binaries are retained in `/tmp/jjs-codec-perf-029830e`.
Diagnostic worktrees remain under `/tmp`; they are not runtime dependencies.

To reproduce the main comparison after building the benchmark binary:

```sh
cd benchmark/compare
go test -c -o /tmp/jjs-codec.test .
SONIC_NO_ASYNC_GC=1 GOMAXPROCS=10 /tmp/jjs-codec.test \
  -test.run '^$' \
  -test.bench '^Benchmark(Decoder_(Binding|Generic|FreshBinding)|Encoder_(Binding|Generic|UnorderedGeneric))$/(Small|Medium|Large)/(goccy|sonic|jjs-go)$' \
  -test.benchmem -test.benchtime=500ms -test.count=3 -test.cpu=10
```
