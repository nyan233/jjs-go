# Apple M1 Pro ARM64 benchmark results

> Historical snapshot for codec revision `cb635eb`, before the typed JIT
> encoder inlining and typed-map optimizations merged to `main`. The current
> report is [RESULTS_ARM64.md](RESULTS_ARM64.md).

Measured on 2026-11-23 with macOS 27.0.1 (26A434), Go 1.25.0,
Apple M1 Pro (10 cores, 32 GiB RAM), and `GOMAXPROCS=10`, on AC power
with Low Power Mode disabled. The measured `main` revision is **`cb635eb`**,
after all merged correctness, decoder, cache and ARM64 quoting/lookup4 changes.
The SSA experiment is excluded. Sonic uses `ConfigStd` with
`SONIC_NO_ASYNC_GC=1`, except for the explicitly unordered encoder below.

Values are medians of three one-second samples, shown as **ns/op (allocs/op)**.
All loops are serial and codecs are warmed before timing. Encoding includes
output allocation. Binding decode reuses a destination; FreshBinding creates
one per iteration; Generic decode resets its destination to nil.
Go reports integer average allocation counts, so a reported zero can coexist
with nonzero B/op when allocation is amortized across operations.
The Windows amd64 results use a different Go version, codec revision and
configuration; compare libraries within each platform's table.

## Setup and methodology

| Item | Value |
| --- | --- |
| Codec revision | `cb635eb071e006a2df6c789a3bc602e47758164d` (`main`) |
| Runtime | Native darwin/arm64, `GOARM64=v8.0`, `CGO_ENABLED=1` |
| Standard library | `encoding/json` from Go 1.25.0 |
| goccy | `github.com/goccy/go-json` v0.11.0 |
| jsoniter | `github.com/json-iterator/go` v1.1.12, `ConfigCompatibleWithStandardLibrary` |
| Sonic | `github.com/bytedance/sonic` v1.15.5, native ARM64 path |
| EasyJSON | `github.com/mailru/easyjson` v0.9.2, existing generated fixture code |
| Fixtures | Small 365 B, Medium 13,023 B, Large 631,514 B |
| Samples | 93 cases × 3 samples = 279 samples, 1 second per sample |

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

The machine remained in normal desktop use with other applications present.
No builds, tests, profilers or other benchmark suites ran concurrently with
measurement. This is not an isolated performance host. Results below retain
all samples and report descriptive variability, not confidence intervals or
hardware-counter measurements.

## Run

Comparison-module correctness tests passed before measurement. The test binary
was built before timing. Equivalent reproduction commands:

```sh
cd benchmark/compare
go test -count=1 ./...
go test -c -o /tmp/jjs-m1pro-compare-cb635eb.test .
SONIC_NO_ASYNC_GC=1 GOMAXPROCS=10 /tmp/jjs-m1pro-compare-cb635eb.test \
  -test.run '^$' -test.bench '^(BenchmarkEncoder_|BenchmarkDecoder_)' \
  -test.benchmem -test.benchtime=1s -test.count=3 -test.cpu=10 \
  > results-darwin-arm64-main-cb635eb.txt
```

The direct `go test` equivalent is:

```sh
cd benchmark/compare
SONIC_NO_ASYNC_GC=1 GOMAXPROCS=10 go test -run '^$' \
  -bench '^(BenchmarkEncoder_|BenchmarkDecoder_)' \
  -benchmem -benchtime=1s -count=3 -cpu=10
```

The [run metadata](results-darwin-arm64-main-cb635eb-metadata.json) records the exact command, binary SHA-256,
revision, timestamps, toolchain and observed power settings.

## Binding encoder

| Library | Small | Medium | Large |
| --- | ---: | ---: | ---: |
| `encoding/json` | 927.3 (2) | 11,309 (2) | 415,566 (1,112) |
| `goccy/go-json` | 557.3 (2) | 4,103 (2) | 216,507 (2) |
| `json-iterator/go` | 1,022 (2) | 21,055 (2) | 659,704 (1,426) |
| Sonic (`ConfigStd`) | 1,597 (3) | 16,544 (3) | 483,994 (107) |
| EasyJSON | 915.2 (6) | 10,742 (67) | 415,904 (2,547) |
| **jjs-go** | **652.5 (2)** | **4,787 (2)** | **172,340 (210)** |

## Binding decoder

| Library | Small | Medium | Large |
| --- | ---: | ---: | ---: |
| `encoding/json` | 4,253 (15) | 89,580 (132) | 3,965,404 (4,903) |
| `goccy/go-json` | 446.7 (0) | 8,521 (1) | 445,444 (444) |
| `json-iterator/go` | 1,122 (18) | 26,750 (351) | 1,353,053 (17,681) |
| Sonic (`ConfigStd`) | 1,358 (10) | 22,352 (110) | 945,391 (4,099) |
| EasyJSON | 1,206 (7) | 19,535 (124) | 1,916,119 (4,338) |
| **jjs-go** | **949.4 (0)** | **17,059 (1)** | **877,257 (444)** |

## Generic encoder

| Library | Small | Medium | Large |
| --- | ---: | ---: | ---: |
| `encoding/json` | 4,206 (52) | 63,492 (653) | 2,856,557 (27,955) |
| `goccy/go-json` | 1,697 (1) | 25,272 (1) | 1,306,152 (1) |
| `json-iterator/go` | 5,160 (66) | 85,042 (613) | 4,299,568 (23,832) |
| Sonic (`ConfigStd`) | 4,327 (7) | 50,643 (40) | 2,279,421 (1,268) |
| **jjs-go** | **1,871 (11)** | **20,740 (77)** | **975,216 (2,529)** |

## Generic decoder

| Library | Small | Medium | Large |
| --- | ---: | ---: | ---: |
| `encoding/json` | 5,018 (92) | 105,414 (794) | 4,968,004 (32,124) |
| `goccy/go-json` | 2,254 (23) | 26,757 (134) | 1,144,130 (4,087) |
| `json-iterator/go` | 4,113 (113) | 65,951 (1,084) | 2,903,149 (44,976) |
| Sonic (`ConfigStd`) | 2,795 (73) | 38,354 (705) | 1,620,922 (28,731) |
| **jjs-go** | **2,672 (23)** | **36,395 (134)** | **1,628,045 (4,084)** |

## Fresh-destination binding decoder

| Library | Small | Medium | Large |
| --- | ---: | ---: | ---: |
| `encoding/json` | 4,766 (26) | 94,062 (160) | 4,057,938 (5,073) |
| `goccy/go-json` | 717.2 (6) | 10,497 (19) | 494,427 (583) |
| `json-iterator/go` | 1,508 (31) | 28,882 (379) | 1,426,606 (17,851) |
| Sonic (`ConfigStd`) | 1,644 (21) | 23,772 (145) | 927,305 (4,374) |
| EasyJSON | 1,487 (14) | 21,576 (128) | 2,004,783 (4,348) |
| **jjs-go** | **1,100 (6)** | **18,563 (19)** | **924,695 (587)** |

## Unordered Generic encoder

| Library | Small | Medium | Large |
| --- | ---: | ---: | ---: |
| `goccy/go-json` | 1,452 (1) | 15,992 (1) | 850,284 (1) |
| Sonic (unsorted, HTML escaping enabled) | 4,112 (7) | 42,331 (40) | 1,874,143 (1,267) |
| **jjs-go** | **1,854 (11)** | **19,140 (77)** | **913,779 (2,529)** |

## Interpretation

jjs-go's Binding encoding is 1.42–2.41x as fast as stdlib in these samples.
Its Large median is below goccy.

jjs-go's Binding decoding is 4.48–5.25x as fast as stdlib in these samples.
goccy has a lower median at all three sizes.

Default Generic encoding includes different map-order behavior: jjs-go does
not sort keys, while stdlib and Sonic `ConfigStd` do. The UnorderedGeneric
control disables map sorting for jjs-go, goccy and Sonic while retaining HTML
escaping and string validation. Use that control when assessing their generic
encoding performance with sorting removed.

These results supersede the earlier ARM64 README table. The
[archived 96ffa5b report](RESULTS_ARM64_96ffa5b.md) and its original raw output
remain available. The Windows amd64 README results use a different Go version,
codec revision and configuration; they remain historical measurements and are
not a controlled architecture comparison with this run.

The focused [decoder/SIMD study](CODEC_OPTIMIZATION_ARM64.md),
[initial assembly quoting study](APPEND_QUOTED_ARM64.md) and
[lookup4 follow-up](QUOTING_PAPERS_ARM64.md) retain their own baselines and
sample durations. Their incremental before/after percentages are separate
from this full-suite snapshot.

## Sample variability

Descriptive ranges across three samples, ordered by `(max - min) / median`.
The ten largest spreads are shown; no sample was removed.

| Case | Min ns/op | Median ns/op | Max ns/op | Spread |
| --- | ---: | ---: | ---: | ---: |
| Encoder_Generic/Large/stdlib | 2,851,781 | 2,856,557 | 4,096,059 | 43.6% |
| Encoder_Generic/Large/jsoniter | 3,568,430 | 4,299,568 | 4,609,364 | 24.2% |
| Decoder_Generic/Large/stdlib | 4,882,319 | 4,968,004 | 6,043,226 | 23.4% |
| Decoder_Binding/Medium/jsoniter | 26,441 | 26,750 | 32,490 | 22.6% |
| Decoder_Generic/Medium/goccy | 26,110 | 26,757 | 30,930 | 18.0% |
| Decoder_Binding/Large/jjs-go | 851,935 | 877,257 | 1,004,721 | 17.4% |
| Decoder_Generic/Large/jjs-go | 1,558,307 | 1,628,045 | 1,828,194 | 16.6% |
| Encoder_Generic/Medium/stdlib | 63,490 | 63,492 | 73,846 | 16.3% |
| Encoder_Binding/Large/goccy | 214,817 | 216,507 | 248,884 | 15.7% |
| Decoder_Generic/Medium/jsoniter | 64,014 | 65,951 | 73,350 | 14.2% |

[Raw benchmark output](results-darwin-arm64-main-cb635eb.txt) contains all 279 samples, including iteration
counts, ns/op, B/op, allocs/op and throughput. README and report medians are
calculated from that same file.

All first-run cases with a spread of at least 20% were rechecked in separate
processes using the same binary and three one-second samples. The published
tables retain the original full-run medians.

| Case | Original median ns/op | Recheck median ns/op | Change |
| --- | ---: | ---: | ---: |
| Decoder_Binding/Medium/jsoniter | 26,750 | 26,475 | -1.0% |
| Encoder_Generic/Large/stdlib | 2,856,557 | 2,891,194 | +1.2% |
| Encoder_Generic/Large/jsoniter | 4,299,568 | 3,488,431 | -18.9% |
| Decoder_Generic/Large/stdlib | 4,968,004 | 4,912,959 | -1.1% |

[Recheck output](results-darwin-arm64-main-cb635eb-check.txt) records the commands
and every additional sample. Rechecks are not substituted for original data.
