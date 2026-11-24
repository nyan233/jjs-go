# Apple M1 Pro ARM64 benchmark results

Measured on 2026-11-24 with macOS 27.0.1 (26A434), Go 1.25.0,
Apple M1 Pro (10 cores, 32 GiB RAM), and `GOMAXPROCS=10`, on AC power
with Low Power Mode disabled. The measured `main` revision is **`2813ca6`**,
after typed JIT encoder inlining, typed map encoders, NEON Base64 and all
previously merged correctness, decoder, cache and ARM64 quoting/lookup4
changes. The SSA experiment is excluded. Sonic uses `ConfigStd` with
`SONIC_NO_ASYNC_GC=1`, except for the explicitly unordered encoder below.

Values are medians of six one-second samples, shown as **ns/op (allocs/op)**.
All loops are serial and codecs are warmed before timing. Encoding includes
output allocation. Binding decode reuses a destination; FreshBinding creates
one per iteration; Generic decode resets its destination to nil.
Go reports integer average allocation counts, so a reported zero can coexist
with nonzero B/op when allocation is amortized across operations.

The machine remained in normal desktop use with all applications running.
Before measurement, the Telegram process — a sustained consumer of about 85%
CPU — was moved to the Darwin background scheduling policy with
`taskpolicy(8)`; it was restored after the run and the recheck. Two
uncontrolled passes measured the unchanged libraries 16–20% slower on average.
A control run of the `cb635eb` binary on the Binding encoder family under the
same demotion reproduced that report's medians for unchanged libraries within
a few percent, so the library-to-library comparisons here remain comparable
with the earlier run.
No builds, tests, profilers or other benchmark suites ran concurrently with
measurement.

## Setup and methodology

| Item | Value |
| --- | --- |
| Codec revision | `2813ca6957cf1797a9ef294e2221de9d6f857181` (`main`) |
| Runtime | Native darwin/arm64, `GOARM64=v8.0`, `CGO_ENABLED=1` |
| Standard library | `encoding/json` from Go 1.25.0 |
| goccy | `github.com/goccy/go-json` v0.11.0 |
| jsoniter | `github.com/json-iterator/go` v1.1.12, `ConfigCompatibleWithStandardLibrary` |
| Sonic | `github.com/bytedance/sonic` v1.15.5, native ARM64 path |
| EasyJSON | `github.com/mailru/easyjson` v0.9.2, existing generated fixture code |
| Fixtures | Small 365 B, Medium 13,023 B, Large 631,514 B |
| Samples | 93 cases × 6 samples = 558 samples, 1 second per sample |
| Host control | Telegram demoted to the Darwin background policy, then restored |

EasyJSON is absent from Generic because it requires generated concrete types.
The 93 cases comprise 18 Binding encoders, 18 Binding decoders, 18 fresh-target
Binding decoders, 15 Generic encoders, 15 Generic decoders and 9 unordered
Generic encoder controls. Timings exclude JIT/type-plan compilation and use
`Marshal`, including output allocation, rather than `MarshalTo` buffer reuse.

Default Generic encoding includes different map-order behavior: jjs-go does
not sort keys, while stdlib and Sonic `ConfigStd` do. The UnorderedGeneric
control disables map sorting for jjs-go, goccy and Sonic while retaining HTML
escaping and string validation. Use that control when assessing their generic
encoding performance with sorting removed.

## Run

Comparison-module correctness tests passed before measurement. The test binary
was built before timing. Equivalent reproduction commands:

```sh
cd benchmark/compare
go test -count=1 ./...
go test -c -o /tmp/jjs-m1pro-compare-2813ca6.test .
SONIC_NO_ASYNC_GC=1 GOMAXPROCS=10 /tmp/jjs-m1pro-compare-2813ca6.test \
  -test.run '^$' -test.bench '^(BenchmarkEncoder_|BenchmarkDecoder_)' \
  -test.benchmem -test.benchtime=1s -test.count=6 -test.cpu=10 \
  > results-darwin-arm64-main-2813ca6.txt
```

The direct `go test` equivalent is:

```sh
cd benchmark/compare
SONIC_NO_ASYNC_GC=1 GOMAXPROCS=10 go test -run '^$' \
  -bench '^(BenchmarkEncoder_|BenchmarkDecoder_)' \
  -benchmem -benchtime=1s -count=6 -cpu=10
```

To reproduce the host control, run `taskpolicy -b -p <Telegram pid>` before the
suite and `taskpolicy -B -p <Telegram pid>` afterward.

The [run metadata](results-darwin-arm64-main-2813ca6-metadata.json) records the
exact command, binary SHA-256, revision, timestamps, toolchain and observed
power settings.

## Binding encoder

| Library | Small | Medium | Large |
| --- | ---: | ---: | ---: |
| `encoding/json` | 897.5 (2) | 10,352 (2) | 410,798 (1,112) |
| `goccy/go-json` | 575.2 (2) | 4,050 (2) | 215,806 (2) |
| `json-iterator/go` | 1,022 (2) | 21,430 (2) | 644,206 (1,426) |
| Sonic (`ConfigStd`) | 1,594 (3) | 16,743 (3) | 485,286 (107) |
| EasyJSON | 914.2 (6) | 10,891 (67) | 414,778 (2,547) |
| **jjs-go** | **485.1 (2)** | **3,414 (2)** | **137,045 (2)** |

## Binding decoder

| Library | Small | Medium | Large |
| --- | ---: | ---: | ---: |
| `encoding/json` | 4,276 (15) | 88,942 (132) | 3,950,088 (4,903) |
| `goccy/go-json` | 434.6 (0) | 8,372 (1) | 441,072 (444) |
| `json-iterator/go` | 1,118 (18) | 26,636 (351) | 1,368,273 (17,681) |
| Sonic (`ConfigStd`) | 1,350 (10) | 22,048 (110) | 899,736 (4,099) |
| EasyJSON | 1,184 (7) | 19,318 (124) | 1,891,829 (4,338) |
| **jjs-go** | **933.3 (0)** | **16,932 (1)** | **852,557 (444)** |

## Generic encoder

| Library | Small | Medium | Large |
| --- | ---: | ---: | ---: |
| `encoding/json` | 4,122 (52) | 63,710 (653) | 2,833,784 (27,955) |
| `goccy/go-json` | 1,678 (1) | 24,935 (1) | 1,285,131 (1) |
| `json-iterator/go` | 5,110 (66) | 84,578 (613) | 3,440,172 (23,834) |
| Sonic (`ConfigStd`) | 4,250 (7) | 50,548 (40) | 2,244,160 (1,268) |
| **jjs-go** | **1,434 (1)** | **15,131 (1)** | **782,752 (1)** |

## Generic decoder

| Library | Small | Medium | Large |
| --- | ---: | ---: | ---: |
| `encoding/json` | 4,888 (92) | 105,178 (794) | 4,854,846 (32,124) |
| `goccy/go-json` | 2,124 (23) | 24,631 (134) | 1,089,802 (4,087) |
| `json-iterator/go` | 4,084 (113) | 63,955 (1,084) | 2,857,082 (44,976) |
| Sonic (`ConfigStd`) | 2,606 (73) | 36,843 (705) | 1,561,852 (28,731) |
| **jjs-go** | **2,633 (23)** | **35,194 (134)** | **1,535,946 (4,084)** |

## Fresh-destination binding decoder

| Library | Small | Medium | Large |
| --- | ---: | ---: | ---: |
| `encoding/json` | 4,748 (26) | 92,368 (160) | 3,999,125 (5,073) |
| `goccy/go-json` | 709.8 (6) | 10,170 (19) | 483,342 (583) |
| `json-iterator/go` | 1,501 (31) | 29,098 (379) | 1,393,796 (17,851) |
| Sonic (`ConfigStd`) | 1,642 (21) | 23,928 (145) | 910,425 (4,374) |
| EasyJSON | 1,448 (14) | 20,795 (128) | 1,985,463 (4,348) |
| **jjs-go** | **1,118 (6)** | **18,094 (19)** | **927,254 (587)** |

## Unordered Generic encoder

| Library | Small | Medium | Large |
| --- | ---: | ---: | ---: |
| `goccy/go-json` | 1,441 (1) | 14,878 (1) | 846,360 (1) |
| Sonic (unsorted, HTML escaping enabled) | 4,034 (7) | 41,912 (40) | 1,907,963 (1,267) |
| **jjs-go** | **1,421 (1)** | **14,771 (1)** | **780,358 (1)** |

## Interpretation

jjs-go's Binding encoding is 1.85–3.03x as fast as stdlib in these samples. It
now has the lowest median in all three sizes: 1.19x faster than goccy at Small
and Medium, 1.57x at Large, and 3.29–4.90x faster than Sonic. The typed JIT
encoder also removes most of the remaining allocations: Binding Large reports
2 allocs/op, down from 210 in the preceding `cb635eb` run.

jjs-go's Binding decoding is 4.58–5.25x as fast as stdlib in these samples and
ahead of Sonic and EasyJSON, but goccy has a lower median at all three sizes.
The direct decoder is unchanged by the typed encoder work.

Generic jjs-go encoding has the lowest median at all three sizes
(1.17–1.65x faster than goccy and 2.87–3.34x faster than Sonic) and drops to
one allocation per operation, down from 11/77/2,529 at Small/Medium/Large in
the preceding run. Generic jjs-go decoding is 1.86–3.16x as fast as stdlib and
level with Sonic; goccy remains ahead.

In the unordered-map control, which disables key sorting for jjs-go, goccy and
Sonic while retaining HTML escaping and string validation, jjs-go is slightly
faster than goccy at Small and Medium and 8% faster at Large, and 2.44–2.84x
faster than Sonic.

The full-suite deltas agree with the isolated before/after measurements in
[JIT_INLINE_ARM64.md](JIT_INLINE_ARM64.md) and
[JIT_MAP_ARM64.md](JIT_MAP_ARM64.md). This run is the comparison snapshot; the
focused reports retain their own baselines, sample durations and code-size
measurements. The Windows amd64 README results use a different Go version,
codec revision and configuration and remain separate measurements.

The [cb635eb report](RESULTS_ARM64_cb635eb.md) and
[96ffa5b report](RESULTS_ARM64_96ffa5b.md) remain available as historical
snapshots. The focused [decoder/SIMD study](CODEC_OPTIMIZATION_ARM64.md),
[assembly quoting study](APPEND_QUOTED_ARM64.md) and
[paper-guided UTF-8 follow-up](QUOTING_PAPERS_ARM64.md) retain their own
baselines.

## Sample variability

Descriptive ranges across the six samples, ordered by `(max - min) / median`.
The ten largest spreads are shown; no sample was removed.

| Case | Min ns/op | Median ns/op | Max ns/op | Spread |
| --- | ---: | ---: | ---: | ---: |
| Encoder_Binding/Medium/jsoniter | 20,713 | 21,430 | 25,009 | 20.0% |
| Encoder_UnorderedGeneric/Large/jjs-go | 776,556 | 780,358 | 895,557 | 15.2% |
| Encoder_Binding/Small/easyjson | 871.0 | 914.2 | 995.4 | 13.6% |
| Decoder_FreshBinding/Small/jsoniter | 1,496 | 1,501 | 1,657 | 10.7% |
| Encoder_Binding/Small/sonic | 1,587 | 1,594 | 1,751 | 10.3% |
| Encoder_Binding/Medium/stdlib | 10,148 | 10,352 | 10,989 | 8.1% |
| Encoder_Binding/Medium/goccy | 3,970 | 4,050 | 4,198 | 5.6% |
| Encoder_Binding/Medium/jjs-go | 3,379 | 3,414 | 3,570 | 5.6% |
| Encoder_Binding/Small/jsoniter | 1,010 | 1,022 | 1,060 | 4.9% |
| Encoder_Binding/Small/goccy | 568.0 | 575.2 | 590.1 | 3.8% |

The only case above the 20% recheck threshold was measured again in a separate
process with the same binary and host controls. The published tables retain
the original full-run median; rechecks are not substituted for original data.

| Case | Original median ns/op | Recheck median ns/op | Change |
| --- | ---: | ---: | ---: |
| Encoder_Binding/Medium/jsoniter | 21,430 | 21,854 | +2.0% |

[Raw benchmark output](results-darwin-arm64-main-2813ca6.txt) contains all 558
samples, including iteration counts, ns/op, B/op, allocs/op and throughput.
README and report medians are calculated from that same file.
[Recheck output](results-darwin-arm64-main-2813ca6-check.txt) records the
command and every additional sample.
