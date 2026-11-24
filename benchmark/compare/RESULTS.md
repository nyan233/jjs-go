# Windows amd64 benchmark results

Measured on 2026-11-24 with:

| Item | Value |
| --- | --- |
| CPU | Intel Core i7-14700KF, 8 physical / 16 logical CPUs visible |
| OS | Windows 11 Pro 10.0.26200 |
| Go | go1.25.0 windows/amd64 |
| `GOEXPERIMENT` | `jsonv2` |
| `GOMAXPROCS` | 16 (default) |
| Sonic GC mode | `SONIC_NO_ASYNC_GC=1` |

Command for Binding, FreshBinding and Generic:

```powershell
$env:GOTOOLCHAIN = "go1.25.0"
$env:GOEXPERIMENT = "jsonv2"
$env:SONIC_NO_ASYNC_GC = "1"
go test -run '^$' -bench '^(BenchmarkEncoder_|BenchmarkDecoder_)' `
    -benchmem -benchtime 1s -count 3
```

Each cell is the median of three samples in
`ns/op (allocs/op)`. Binding, FreshBinding and Generic come from
[`results-windows-amd64-jsonv2.txt`](results-windows-amd64-jsonv2.txt).
UnorderedGeneric was rerun separately with jsonv2 enabled and is retained in
[`results-windows-amd64-jsonv2-unordered.txt`](results-windows-amd64-jsonv2-unordered.txt).

`encoding/json/v2` is the experimental standard-library v2 implementation.
The benchmark uses `FormatNilMapAsNull`, `FormatNilSliceAsNull`,
`MatchCaseInsensitiveNames` and `AllowInvalidUTF8` so its fixture semantics
remain comparable with v1. Ordinary builds without `GOEXPERIMENT=jsonv2`
automatically omit this codec.

EasyJSON is absent from Generic because generic values have no generated
concrete type. Sonic uses `ConfigStd`, except in the explicitly unordered
control.

## Binding encoder

| Library | Small | Medium | Large |
| --- | ---: | ---: | ---: |
| `encoding/json` | 1,317 (3) | 15,526 (5) | 452,066 (468) |
| `encoding/json/v2` | 1,540 (3) | 14,499 (5) | 435,799 (468) |
| `goccy/go-json` | 590.5 (2) | 4,163 (2) | 215,982 (2) |
| `json-iterator/go` | 805.5 (2) | 12,261 (2) | 464,880 (1,426) |
| Sonic (`ConfigStd`) | 552.5 (3) | 4,473 (3) | 169,816 (108) |
| EasyJSON | 748.5 (6) | 13,244 (69) | 484,477 (3,116) |
| `jjs-go` | 517.2 (2) | 7,731 (2) | 304,442 (2) |

jjs-go beats both stdlib versions at every size. goccy and Sonic remain faster
on Medium and Large.

## Binding decoder

| Library | Small | Medium | Large |
| --- | ---: | ---: | ---: |
| `encoding/json` | 2,290 (0) | 30,592 (42) | 1,647,209 (4,419) |
| `encoding/json/v2` | 1,789 (0) | 22,575 (64) | 1,165,944 (3,336) |
| `goccy/go-json` | 379.2 (0) | 7,800 (1) | 411,481 (444) |
| `json-iterator/go` | 935.1 (18) | 20,015 (351) | 1,047,214 (17,681) |
| Sonic (`ConfigStd`) | 744.3 (9) | 12,695 (109) | 558,028 (4,100) |
| EasyJSON | 1,064 (7) | 17,509 (124) | 1,048,628 (4,338) |
| `jjs-go` | 802.4 (0) | 14,736 (1) | 749,916 (444) |

jjs-go beats v1 and v2 at every size. jsonv2 lands between Sonic and jjs-go;
goccy and Sonic remain faster.

## Fresh-destination binding decoder

| Library | Small | Medium | Large |
| --- | ---: | ---: | ---: |
| `encoding/json` | 2,797 (11) | 33,210 (70) | 1,716,765 (4,589) |
| `encoding/json/v2` | 2,238 (11) | 24,590 (68) | 1,223,510 (3,345) |
| `goccy/go-json` | 638.7 (6) | 9,537 (19) | 454,738 (583) |
| `json-iterator/go` | 1,260 (31) | 22,050 (379) | 1,121,820 (17,851) |
| Sonic (`ConfigStd`) | 1,000 (17) | 13,901 (128) | 594,142 (4,245) |
| EasyJSON | 1,319 (14) | 19,470 (128) | 1,076,136 (4,347) |
| `jjs-go` | 1,006 (6) | 16,883 (19) | 805,157 (587) |

## Generic encoder

| Library | Small | Medium | Large |
| --- | ---: | ---: | ---: |
| `encoding/json` | 3,611 (26) | 34,131 (8) | 1,622,860 (9) |
| `encoding/json/v2` | 2,939 (15) | 19,390 (6) | 951,765 (6) |
| `goccy/go-json` | 1,560 (1) | 22,994 (1) | 1,178,919 (1) |
| `json-iterator/go` | 4,496 (66) | 68,714 (613) | 2,947,162 (23,833) |
| Sonic (`ConfigStd`) | 1,565 (7) | 22,233 (40) | 1,010,284 (1,267) |
| `jjs-go` | 1,371 (1) | 14,792 (1) | 948,404 (1) |

jjs-go has the lowest median at all three sizes and is narrowly ahead of
jsonv2 on Large.

## Generic decoder

| Library | Small | Medium | Large |
| --- | ---: | ---: | ---: |
| `encoding/json` | 7,907 (102) | 93,631 (930) | 4,015,852 (38,275) |
| `encoding/json/v2` | 3,748 (71) | 47,802 (678) | 2,250,367 (28,099) |
| `goccy/go-json` | 1,838 (23) | 19,827 (134) | 889,863 (4,087) |
| `json-iterator/go` | 3,584 (113) | 47,414 (1,084) | 2,087,374 (44,976) |
| Sonic (`ConfigStd`) | 2,698 (74) | 35,520 (747) | 1,488,211 (30,645) |
| `jjs-go` | 2,326 (23) | 27,476 (134) | 1,290,195 (4,083) |

jjs-go beats v1, v2 and Sonic at every size. goccy remains ahead.

## Unordered Generic encoder

| Library | Small | Medium | Large |
| --- | ---: | ---: | ---: |
| `encoding/json/v2` | 3,232 (15) | 29,585 (6) | 1,373,985 (6) |
| `goccy/go-json` | 1,231 (1) | 12,024 (1) | 711,338 (1) |
| Sonic (unsorted, HTML escaping enabled) | 1,237 (7) | 13,057 (40) | 727,125 (1,267) |
| `jjs-go` | 1,314 (1) | 15,589 (1) | 951,546 (1) |

Default map ordering is not normalized across codecs. jjs-go and jsonv2 use
their normal unordered iteration; goccy and Sonic use explicit unordered
options in this control.

## Codegen metrics

`go run ./cmd/codegenstats` currently reports for `TwitterResponse`:

| Metric | Encoder | Decoder |
| --- | ---: | ---: |
| Instructions before | 2,205 | 0 |
| Instructions after | 2,073 | 0 |
| Folded instructions | 132 | 0 |
| Instruction reduction | 5.99% | 0.00% |
| Virtual registers | 96 | 0 |
| Register reuse | 92 | 0 |
| Spills | 0 | 0 |
| Generated code bytes | 9,139 | 0 |
| Direct-plan nodes | 0 | 22 |
| Direct-plan fields | 0 | 85 |
