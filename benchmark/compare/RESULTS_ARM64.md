# Apple M1 Pro ARM64 benchmark results

Measured on 2026-11-23 with macOS 27.0.1 (26A434), Go 1.25.0,
Apple M1 Pro (10 cores, 32 GiB RAM), and `GOMAXPROCS=10`. The machine was
on AC power with Low Power Mode disabled. This uses codec commit `96ffa5b`
after the hot-path optimizations. Sonic uses `ConfigStd` with
`SONIC_NO_ASYNC_GC=1`; EasyJSON is absent from Generic.

These are medians of three one-second samples, shown as `ns/op (allocs/op)`.
All benchmark loops are serial; `GOMAXPROCS=10` also permits background GC
workers. The earlier Windows results use a different Go version, settings
and codec revision, so the two platforms are separate measurements rather
than a controlled architecture comparison.

## Setup and methodology

| Item | Value |
| --- | --- |
| Codec revision | `96ffa5b7861be2126cdf73ea95ef0139c0229610` |
| Runtime | Native darwin/arm64, `GOARM64=v8.0`, `CGO_ENABLED=1` |
| Standard library | `encoding/json` from Go 1.25.0 |
| goccy | `github.com/goccy/go-json` v0.11.0 |
| jsoniter | `github.com/json-iterator/go` v1.1.12, `ConfigCompatibleWithStandardLibrary` |
| Sonic | `github.com/bytedance/sonic` v1.15.5, native ARM64 path, `ConfigStd` |
| EasyJSON | `github.com/mailru/easyjson` v0.9.2, existing generated fixture code |
| Fixtures | Small 365 B, Medium 13,023 B, Large 631,514 B |
| Samples | 84 cases × 3 samples, 1 second per sample |

Every codec is warmed before timing. Binding decode reuses one destination;
FreshBinding creates a zero-value destination each iteration. Generic decode
resets its destination to nil. Timings exclude JIT/type-plan compilation.
Encoding uses the existing `Marshal` APIs, including output allocation, rather
than the new `MarshalTo` reuse API.

Map ordering is not normalized. jjs-go iterates maps without sorting, while
stdlib and Sonic `ConfigStd` sort keys. Correctness checks compare decoded
values; they do not require identical map-key order. Generic encoding timings
therefore compare these configured behaviors, not identical ordering work.

The machine remained in normal desktop use, with browser and other background
processes present; results are not from an isolated performance host. No other
builds or benchmark suites were launched alongside this run. Sample spreads
are reported below; small differences should not be treated as firm rankings.

## Run

All comparison-module correctness tests passed with `go test ./...` before
measurement. The published run compiled the test binary first:

```sh
cd benchmark/compare
go test -c -o /tmp/jjs-m1pro-compare-96ffa5b.test .
SONIC_NO_ASYNC_GC=1 GOMAXPROCS=10 /tmp/jjs-m1pro-compare-96ffa5b.test \
  -test.run '^$' -test.bench '^(BenchmarkEncoder_|BenchmarkDecoder_)' \
  -test.benchmem -test.benchtime=1s -test.count=3 -test.cpu=10 \
  > results-darwin-arm64-m1pro.txt
```

The equivalent direct `go test` command is:

```sh
cd benchmark/compare
SONIC_NO_ASYNC_GC=1 GOMAXPROCS=10 go test -run '^$' \
  -bench '^(BenchmarkEncoder_|BenchmarkDecoder_)' \
  -benchmem -benchtime=1s -count=3 -cpu=10
```

## Binding encoder

| Library | Small | Medium | Large |
| --- | ---: | ---: | ---: |
| `encoding/json` | 919.3 (2) | 10,592 (2) | 421,077 (1,112) |
| `goccy/go-json` | 558.3 (2) | 4,032 (2) | 217,601 (2) |
| `json-iterator/go` | 1,020 (2) | 20,978 (2) | 658,874 (1,426) |
| Sonic (`ConfigStd`) | 1,602 (3) | 16,964 (3) | 490,437 (107) |
| EasyJSON | 909.4 (6) | 11,006 (67) | 416,020 (2,547) |
| **jjs-go** | **686.2 (2)** | **6,512 (2)** | **269,039 (210)** |

## Binding decoder

| Library | Small | Medium | Large |
| --- | ---: | ---: | ---: |
| `encoding/json` | 4,350 (15) | 88,690 (132) | 3,952,905 (4,903) |
| `goccy/go-json` | 437.5 (0) | 8,380 (1) | 443,131 (444) |
| `json-iterator/go` | 1,128 (18) | 26,572 (351) | 1,347,108 (17,681) |
| Sonic (`ConfigStd`) | 1,348 (10) | 22,456 (110) | 910,042 (4,099) |
| EasyJSON | 1,199 (7) | 19,700 (124) | 1,901,123 (4,338) |
| **jjs-go** | **1,447 (7)** | **27,472 (107)** | **1,452,808 (4,236)** |

## Generic encoder

| Library | Small | Medium | Large |
| --- | ---: | ---: | ---: |
| `encoding/json` | 4,285 (52) | 64,228 (653) | 2,894,691 (27,955) |
| `goccy/go-json` | 1,730 (1) | 25,285 (1) | 1,331,115 (1) |
| `json-iterator/go` | 5,183 (66) | 86,850 (613) | 3,533,005 (23,833) |
| Sonic (`ConfigStd`) | 4,253 (7) | 51,589 (40) | 2,250,908 (1,268) |
| **jjs-go** | **1,978 (11)** | **21,817 (77)** | **1,067,233 (2,529)** |

## Generic decoder

| Library | Small | Medium | Large |
| --- | ---: | ---: | ---: |
| `encoding/json` | 4,735 (92) | 102,415 (794) | 4,775,992 (32,124) |
| `goccy/go-json` | 2,099 (23) | 24,208 (134) | 1,046,423 (4,087) |
| `json-iterator/go` | 3,956 (113) | 62,237 (1,084) | 2,835,082 (44,976) |
| Sonic (`ConfigStd`) | 2,524 (73) | 35,820 (705) | 1,530,981 (28,731) |
| **jjs-go** | **3,480 (72)** | **60,052 (747)** | **2,764,860 (30,873)** |

## Fresh-destination binding decoder

| Library | Small | Medium | Large |
| --- | ---: | ---: | ---: |
| `encoding/json` | 4,754 (26) | 91,192 (160) | 4,045,352 (5,073) |
| `goccy/go-json` | 714.4 (6) | 11,002 (19) | 491,495 (583) |
| `json-iterator/go` | 1,506 (31) | 29,480 (379) | 1,405,177 (17,851) |
| Sonic (`ConfigStd`) | 1,659 (21) | 24,757 (145) | 933,857 (4,374) |
| EasyJSON | 1,479 (14) | 21,745 (128) | 1,977,472 (4,348) |
| **jjs-go** | **1,757 (13)** | **29,512 (125)** | **1,533,382 (4,379)** |

## Interpretation

Typed jjs-go encoding ranks second at all three sizes, behind goccy, and is
1.34–1.63x as fast as stdlib. Typed decoding is 2.72–3.23x as fast as stdlib,
but still trails goccy and Sonic. Generic encoding has the lowest reported
median on Medium and Large under these codec configurations, with the map
ordering difference described above.

The Windows table in the root README remains the 2026-11-21 result. The
earlier [hot-path before/after study](HOTPATH_OPTIMIZATION.md) uses
`GOMAXPROCS=1`, shorter samples and only stdlib controls, so its absolute
numbers should also be kept separate from this multi-library run.

## Sample variability

These are descriptive ranges across three samples, not confidence intervals.
The largest ten spreads are shown as `(max - min) / median`:

| Case | Min ns/op | Median ns/op | Max ns/op | Spread |
| --- | ---: | ---: | ---: | ---: |
| Encoder_Generic/Medium/sonic | 51,030 | 51,589 | 75,816 | 48.0% |
| Decoder_Binding/Large/easyjson | 1,898,925 | 1,901,123 | 2,490,005 | 31.1% |
| Decoder_FreshBinding/Medium/jsoniter | 29,335 | 29,480 | 36,801 | 25.3% |
| Decoder_FreshBinding/Medium/goccy | 10,523 | 11,002 | 12,531 | 18.3% |
| Encoder_Binding/Large/goccy | 213,426 | 217,601 | 249,611 | 16.6% |
| Decoder_FreshBinding/Large/sonic | 925,113 | 933,857 | 1,050,967 | 13.5% |
| Encoder_Binding/Medium/jjs-go | 6,193 | 6,512 | 7,028 | 12.8% |
| Decoder_Binding/Large/goccy | 439,763 | 443,131 | 488,028 | 10.9% |
| Encoder_Generic/Medium/jjs-go | 21,209 | 21,817 | 23,495 | 10.5% |
| Encoder_Binding/Medium/sonic | 16,388 | 16,964 | 18,064 | 9.9% |

[Raw benchmark output](results-darwin-arm64-m1pro.txt) contains every sample,
including `B/op`, `allocs/op`, throughput and iteration counts. Medians in
this report and the root README were calculated from that file; no sample
was removed.

All three cases with a first-run spread of at least 20% were rerun in separate
processes using the same binary, settings and three one-second samples:

| Case | Original median ns/op | Recheck median ns/op | Change |
| --- | ---: | ---: | ---: |
| Decoder_Binding/Large/easyjson | 1,901,123 | 1,864,339 | -1.9% |
| Decoder_FreshBinding/Medium/jsoniter | 29,480 | 28,499 | -3.3% |
| Encoder_Generic/Medium/sonic | 51,589 | 49,540 | -4.0% |

The published tables retain the original medians. The
[recheck output](results-darwin-arm64-m1pro-check.txt) includes the exact
commands and all nine additional samples.
