# Decoder bottlenecks on Apple M1 Pro

Measured on 2026-11-23, at documentation revision `029830e` (codec revision
`96ffa5b`), with Go 1.25.0 on native darwin/arm64. Hardware, library versions,
fixtures and codec settings match [the published ARM64 run](RESULTS_ARM64_96ffa5b.md).
This is a diagnosis: the experiments below have **not** been applied to the
project's runtime implementation at that revision. The subsequent implementation
and measurements are recorded in [the optimization study](CODEC_OPTIMIZATION_ARM64.md).

## Observed gap

Published medians, in microseconds per operation:

| Decode case | jjs-go | goccy v0.11.0 | Sonic v1.15.5 | jjs/goccy | jjs/Sonic |
| --- | ---: | ---: | ---: | ---: | ---: |
| Binding / Small | 1.447 | 0.438 | 1.348 | 3.31x | 1.07x |
| Binding / Medium | 27.472 | 8.380 | 22.456 | 3.28x | 1.22x |
| Binding / Large | 1,452.808 | 443.131 | 910.042 | 3.28x | 1.60x |
| Generic / Large | 2,764.860 | 1,046.423 | 1,530.981 | 2.64x | 1.81x |

The benchmark warms every library before timing. Binding reuses its typed
destination; Generic starts with a nil destination each iteration. Compilation
and cold cache misses do not explain these numbers.

Although the entry point is named `compileJITDecoder`, these fixtures use the
cached direct decode plan and Go recursive parser: see
[`compileJITDecoder`](../../core/compiler/decoder.go) and
[`decodeDirect`](../../core/compiler/direct_decoder.go). The profiles confirm
this path. Optimizing ARM64 machine-code emission would not accelerate this
measured decoder path.

## CPU and allocation evidence

Eight cases were profiled separately for six seconds each: jjs Binding
Small/Medium/Large, goccy and Sonic Binding Large, and all three Generic Large.
No other builds or benchmark suites ran alongside the measurements.

The macOS CPU profiles include substantial runtime polling/scheduling samples.
The percentages below use `pprof -focus='benchmark(Binding|Generic)Decode'
-relative_percentages`, so they describe samples on benchmark caller stacks,
not total process CPU or percentages of elapsed time. Background GC is excluded.
Cumulative percentages overlap and must not be added together. Heap attribution
uses statistical sampling; exact per-operation counts come from `-benchmem`.

| Case | Function/path | Share of focused CPU samples |
| --- | --- | ---: |
| Binding / Small | `runtime.(*_panic).nextDefer` | 15.0% cumulative |
| Binding / Small | `runtime.deferprocStack` | 2.0% flat |
| Binding / Medium | `skipSpace` | 27.8% flat |
| Binding / Large | `parseStringInternal` | 46.6% cumulative |
| Binding / Large | `skipSpace` | 19.5% flat |
| Binding / Large | `skipDirectValue` | 33.3% cumulative, includes string/space work |
| Binding / Large | `lookupField` | 9.0% cumulative |
| Generic / Large | `parseStringInternal` | 31.0% cumulative |
| Generic / Large | `runtime.mapassign_faststr` | 31.7% cumulative |
| Generic / Large | `runtime.mallocgc` | 28.5% cumulative |
| Generic / Large | map table growth | 16.0% cumulative, overlaps insertion/allocation |

### Recursive defer bookkeeping

`decodeDirect`, `decodeDirectAny` and `skipDirectValue` increment parser depth
and defer its decrement. Each function has many return sites. Go 1.25's
`cmd/compile/internal/ssagen/ssa.go` disables open-coded defers when
`NumReturns * NumDefers > 15`. Disassembly confirms that the baseline calls
`runtime.deferprocStack` and runtime deferred-return machinery on this hot path.
The `_panic` symbols here also serve normal deferred returns; they do not mean
the benchmark is panicking.

Moving the existing depth check and defer into a small wrapper, with the parser
body in a separate method, enables open-coded defer without changing the depth
limit or removing cleanup. This is the strongest small-change experiment below.

### Scanning and skipping dominate large typed inputs

[`parseStringInternal`](../../core/compiler/jit_json_parser.go) scans the first
eight bytes individually on ARM64, then uses an eight-byte word scanner.
[`scanJSONStringEnd`](../../core/compiler/json_scan_arm64_impl.go) uses scalar
integer masks, not NEON. Non-ASCII bytes return to rune decoding. Strings,
object keys, UTF-8 handling and whitespace all pass through these paths.

The parser also repeats a scanner call at the special byte just found when
`special > 0`. Removing that repeat is simple, but its measured effect is small;
it is not the explanation for the overall 3x gap.

The Large typed fixture contains many fields absent from the destination type.
`skipDirectValue` still recursively parses their grammar and scans their keys
and strings. For escaped strings it calls `parseStringSlow`, materializes an
unescaped string and immediately discards it. Heap samples attributed about
4% of Binding Large allocation objects to this skip path. A validation-preserving
skip-string path can avoid creating these unused values.

### Generic construction adds allocation and growth costs

`decodeDirectAny` separately allocates string contents and interface boxes,
starts maps with capacity eight and inserts while parsing, and starts arrays
with capacity eight even before checking whether they are empty. Maps and
slices then grow as needed. The Generic Large profile shows both the parser
cost and substantial map growth, hashing, boxing and allocation cost.

Published allocation counts per operation:

| Decode case | jjs-go | goccy | Sonic |
| --- | ---: | ---: | ---: |
| Binding / Small | 7 | 0 | 10 |
| Binding / Medium | 107 | 1 | 110 |
| Binding / Large | 4,236 | 444 | 4,099 |
| Generic / Large | 30,873 | 4,087 | 28,731 |

goccy's displayed zero is an amortized, rounded allocation count; it does not
mean strings are never copied. Sonic's Binding Large allocates about 829 KB/op
versus jjs's 203 KB/op and still runs faster. Allocation count or bytes alone
therefore cannot explain the Sonic gap.

## Why the other libraries are faster on this workload

Source inspection used the exact modules selected by `benchmark/compare/go.mod`.

**goccy v0.11.0** combines several mechanisms:

- `internal/decoder/context.go`: pooled padded input scratch; short strings are
  copied into append-only arena chunks. The default benchmark does not enable
  borrowed input strings. Already-returned arena spans are not overwritten.
- The same context batches float and string interface boxes in typed slabs,
  reducing individual heap allocations.
- `internal/decoder/interface.go`: temporarily stages map entries/array elements,
  then allocates the final container with its known size. Empty arrays share a
  zero-capacity interface value. Duplicate map keys retain last-value-wins order.
- `internal/decoder/struct.go` and `struct_keys.go`: specialized field decoders
  and integrated key scanning/lookup; scalar cursors and fast skip paths avoid
  much of jjs's generic recursive bookkeeping.

These mechanisms explain where goccy saves work; their individual contributions
have not been isolated by modifying goccy. Its advantage should not be attributed
to a JIT or to simply turning off string copying.

**Sonic v1.15.5 on ARM64** selects `optdec.Decode` in
`internal/decoder/api/decoder_arm64.go`, rather than the amd64 JIT decoder.
Its native parser builds a node representation, then precompiled type decoders
assign from nodes. Parsing and UTF-8 validation use the NEON native backend.
In Binding Large, native parsing and UTF-8 validation account for about 39% and
26% of focused samples respectively. Unknown fields can be skipped through
validated nodes during assignment. This architecture has upfront work and extra
storage, but its bulk parsing is effective on larger inputs. jjs takes about
7% more time than Sonic on Small and 60% more on Large.

## Controlled single-change experiments

Experiments used a detached temporary worktree at `029830e`. Each variant changes
one mechanism relative to the same baseline; changes were not combined. All
three variants passed the existing root and comparison-module tests before
building their benchmark binaries. Measurements use serial benchmark loops,
`SONIC_NO_ASYNC_GC=1`, `GOMAXPROCS=10`, five 300 ms samples per case, rotating
variant order between rounds. The machine remained in normal desktop use.

The table shows medians and elapsed-time changes relative to this experiment's
baseline, not the earlier published run. Negative percentages mean less time.

| Case | Baseline ns/op | Small defer wrappers | Avoid repeated string scan | Local whitespace cursor |
| --- | ---: | ---: | ---: | ---: |
| Binding / Small | 1,420 | 955.5 (-32.7%) | 1,424 (+0.3%) | 1,467 (+3.3%) |
| Binding / Medium | 27,374 | 23,072 (-15.7%) | 26,551 (-3.0%) | 24,576 (-10.2%) |
| Binding / Large | 1,450,296 | 1,363,978 (-6.0%) | 1,428,623 (-1.5%) | 1,292,290 (-10.9%) |
| Generic / Small | 3,705 | 3,037 (-18.0%) | 3,689 (-0.4%) | 3,725 (+0.5%) |
| Generic / Medium | 63,727 | 56,658 (-11.1%) | 62,929 (-1.3%) | 59,919 (-6.0%) |
| Generic / Large | 2,859,817 | 2,602,571 (-9.0%) | 2,845,278 (-0.5%) | 2,713,761 (-5.1%) |

Allocation counts were unchanged. Within-case sample ranges were 1.0–3.4% of
the median; the small string-scan differences need longer measurements before
being treated as firm gains. The whitespace variant regresses Small, so its
no-whitespace path needs attention. Independent savings cannot be added together
or used as a prediction for a combined implementation.

A separate fixture diagnostic removed formatting whitespace using `json.Compact`
outside the timer. Medium and Large contain 31.6% and 26.1% formatting whitespace
respectively; Small contains none. Three 300 ms samples per case gave:

| Binding case | Library | Original ns/op | Compact ns/op | Time change |
| --- | --- | ---: | ---: | ---: |
| Medium | jjs-go | 27,408 | 20,789 | -24.1% |
| Medium | goccy | 8,546 | 6,417 | -24.9% |
| Medium | Sonic | 22,465 | 18,266 | -18.7% |
| Large | jjs-go | 1,449,521 | 1,093,449 | -24.6% |
| Large | goccy | 431,769 | 354,871 | -17.8% |
| Large | Sonic | 894,186 | 720,991 | -19.4% |

This is an input diagnostic, not an end-to-end optimization: compaction cost
is excluded. It preserves keys, unknown fields, strings, number spelling and
field order. Whitespace matters, but jjs remains about 3.1–3.2x slower than
goccy after removing it.

## Recommended order of work

1. **Small depth/defer wrappers.** Strong measured benefit, limited change, no
   API change. Preserve error/panic cleanup and the existing nesting limit.
2. **Whitespace cursor and call sites.** Keep a cheap no-whitespace path;
   use a local cursor for runs and reduce redundant checks at parser boundaries.
   Benchmark compact and formatted inputs separately.
3. **String scanning and unknown-value skipping.** Separate validation from
   materializing string output. Then evaluate an ARM64 NEON scanner with safe
   tail handling and UTF-8/escape behavior matching the current contract. The
   tiny repeated-scan fix alone has limited impact.
4. **Generic container construction, then allocation batching.** Measure staged
   map/slice construction, empty-array handling, short-string arena storage and
   typed boxing slabs separately. Preserve ownership of returned data across
   future decodes; test retained results, input mutation, GC and concurrency.
5. **Field lookup.** Exact misses currently fall through to a linear
   `strings.EqualFold` scan. Cache folded matches or fuse key scanning with
   lookup while preserving exact-match precedence, Unicode folding and field
   selection order. This is useful but smaller than the large-input scan paths.

Full machine-code decoder generation is a larger follow-up, after removing
common parser costs. Warm type-cache lookup and typed integer parsing are not
the leading bottlenecks in these profiles. Generic numeric parsing still has
a scan-plus-`ParseFloat` path, but it is lower priority on these fixtures.

## Reproduction and artifacts

[Raw measurements](results-darwin-arm64-decoder-probes.txt) include every A/B
sample, each diagnostic patch, the compact-fixture harness/output, and focused
profile summaries. No samples were removed.

The full local CPU/heap profiles and prebuilt binaries are retained under
`/tmp/jjs-decoder-analysis-029830e`; the isolated diagnostic worktree is
`/tmp/jjs-decoder-probes-029830e`. They are temporary local artifacts, not
repository dependencies. The profile command for one case is:

```sh
cd benchmark/compare
go test -c -o /tmp/jjs-decoder-baseline.test .
SONIC_NO_ASYNC_GC=1 GOMAXPROCS=10 /tmp/jjs-decoder-baseline.test \
  -test.run '^$' -test.bench '^BenchmarkDecoder_Binding$/^Large$/^jjs-go$' \
  -test.benchmem -test.benchtime=6s -test.count=1 -test.cpu=10 \
  -test.cpuprofile=/tmp/jjs-decoder-large.cpu.pprof \
  -test.memprofile=/tmp/jjs-decoder-large.mem.pprof
go tool pprof -top -relative_percentages \
  -focus='benchmark(Binding|Generic)Decode' \
  /tmp/jjs-decoder-baseline.test /tmp/jjs-decoder-large.cpu.pprof
```
