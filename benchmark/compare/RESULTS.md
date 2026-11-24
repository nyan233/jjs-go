# Windows amd64 benchmark results

Measured on 2026-11-25 with:

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
go test -run '^$' -bench '^Benchmark(Encoder|Decoder)_' `
    -benchmem -benchtime 500ms -count 3
```

Each cell is the median of three samples in `ns/op (allocs/op)` from the
standard-compatible map-key refresh. The checked-in raw sample files retain
earlier measurement runs and are not the source of the refreshed tables.
UnorderedGeneric is measured separately because jjs-go now uses sorted output.

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
| `encoding/json` | 1,264 (3) | 12,113 (5) | 429,588 (468) |
| `encoding/json/v2` | 1,323 (3) | 12,859 (5) | 452,773 (468) |
| `goccy/go-json` | 499.3 (2) | 3,503 (2) | 191,976 (2) |
| `json-iterator/go` | 703.8 (2) | 9,110 (2) | 427,243 (1,426) |
| Sonic (`ConfigStd`) | 434.0 (3) | 3,551 (3) | 145,878 (108) |
| EasyJSON | 766.6 (6) | 10,139 (69) | 464,955 (3,116) |
| `jjs-go` | 417.6 (2) | 3,424 (2) | 224,933 (2) |

jjs-go is fastest on Small and Medium and remains ahead of v1, v2,
json-iterator and EasyJSON on Large. Sonic and goccy are faster on Large.

## Binding decoder

| Library | Small | Medium | Large |
| --- | ---: | ---: | ---: |
| `encoding/json` | 2,137 (0) | 28,132 (42) | 1,537,876 (4,419) |
| `encoding/json/v2` | 1,692 (0) | 22,087 (64) | 1,094,153 (3,336) |
| `goccy/go-json` | 354.4 (0) | 7,908 (1) | 383,788 (444) |
| `json-iterator/go` | 893.8 (18) | 19,213 (351) | 1,012,733 (17,681) |
| Sonic (`ConfigStd`) | 695.2 (9) | 11,561 (109) | 546,878 (4,100) |
| EasyJSON | 1,028 (7) | 17,074 (124) | 1,004,336 (4,338) |
| `jjs-go` | 850.4 (0) | 14,358 (1) | 702,576 (444) |

jjs-go beats v1, v2, json-iterator and EasyJSON at every size. goccy and
Sonic remain faster.

## Fresh-destination binding decoder

| Library | Small | Medium | Large |
| --- | ---: | ---: | ---: |
| `encoding/json` | 2,636 (11) | 30,518 (70) | 1,596,443 (4,589) |
| `encoding/json/v2` | 2,101 (11) | 22,870 (68) | 1,132,883 (3,345) |
| `goccy/go-json` | 633.9 (6) | 9,389 (19) | 406,406 (583) |
| `json-iterator/go` | 1,220 (31) | 21,080 (379) | 1,072,531 (17,851) |
| Sonic (`ConfigStd`) | 1,043 (17) | 18,140 (128) | 667,420 (4,245) |
| EasyJSON | 1,255 (14) | 18,141 (128) | 1,038,458 (4,347) |
| `jjs-go` | 1,006 (6) | 15,228 (19) | 746,896 (587) |

## Generic encoder

| Library | Small | Medium | Large |
| --- | ---: | ---: | ---: |
| `encoding/json` | 3,401 (26) | 34,405 (8) | 1,490,976 (9) |
| `encoding/json/v2` | 3,195 (15) | 28,704 (6) | 1,341,166 (6) |
| `goccy/go-json` | 1,465 (1) | 21,272 (1) | 1,119,223 (1) |
| `json-iterator/go` | 4,576 (66) | 70,094 (613) | 2,724,759 (23,832) |
| Sonic (`ConfigStd`) | 1,645 (7) | 21,979 (40) | 946,197 (1,267) |
| `jjs-go` | 2,040 (1) | 30,908 (1) | 1,474,881 (1) |

Standard-compatible sorting makes jjs-go slower than goccy, Sonic and jsonv2
in Generic encoding while retaining a lead over v1 and json-iterator.

## Generic decoder

| Library | Small | Medium | Large |
| --- | ---: | ---: | ---: |
| `encoding/json` | 7,393 (102) | 85,712 (930) | 3,943,236 (38,275) |
| `encoding/json/v2` | 3,801 (71) | 53,230 (678) | 2,514,917 (28,099) |
| `goccy/go-json` | 1,733 (23) | 19,915 (134) | 882,685 (4,087) |
| `json-iterator/go` | 3,332 (113) | 48,356 (1,084) | 2,129,693 (44,976) |
| Sonic (`ConfigStd`) | 2,583 (74) | 37,785 (747) | 1,491,083 (30,645) |
| `jjs-go` | 2,274 (23) | 28,200 (134) | 1,290,178 (4,083) |

jjs-go has the lowest Medium median and trails only goccy at all sizes.

## Unordered Generic encoder

| Library | Small | Medium | Large |
| --- | ---: | ---: | ---: |
| `encoding/json/v2` | 3,212 (15) | 29,674 (6) | 1,383,649 (6) |
| `goccy/go-json` | 1,242 (1) | 12,093 (1) | 707,808 (1) |
| Sonic (unsorted, HTML escaping enabled) | 1,262 (7) | 13,034 (40) | 674,897 (1,267) |
jjs-go now matches the standard library's sorted map-key behavior and is no
longer included in this unordered control. The historical jjs-go row was
removed because it measured the previous unordered implementation.

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
