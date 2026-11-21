# Windows amd64 benchmark results

The encoder's sub-256-byte string scanner was subsequently optimized. See
[`SHORT_STRING_SCAN.md`](SHORT_STRING_SCAN.md) for the controlled A/B results.

Measured on 2026-11-21 with:

| Item | Value |
| --- | --- |
| CPU | Intel Core i7-14700KF, 8 physical / 16 logical CPUs visible |
| OS | Windows 11 Pro 10.0.26200 |
| Go | go1.22.0 windows/amd64 |
| `CGO_ENABLED` | 0 |
| `GOMAXPROCS` | 16 (default) |
| Sonic GC mode | `SONIC_NO_ASYNC_GC=1` |

Command:

```powershell
go test -run '^$' -bench '.*_(Binding|Generic)' `
    -benchmem -benchtime 1s -count 2
```

Each table cell is the mean of two samples in the form
`ns/op (allocs/op)`. Raw data is in
[`results-windows-amd64-custom-codecs.txt`](results-windows-amd64-custom-codecs.txt).

## Fixture contamination correction

EasyJSON's default generator previously emitted standard
`MarshalJSON`/`UnmarshalJSON` methods on the shared fixtures. That caused
`encoding/json`, Sonic, goccy, jsoniter and `jjs-go` to call EasyJSON-generated
code for the typed fixtures. Those earlier results were invalid and have been
removed.

EasyJSON is now generated with `-no_std_marshalers`. A regression test verifies
that the shared fixture types do not implement the standard JSON codec
interfaces.

## Binding encoder

| Library | Small | Medium | Large |
| --- | ---: | ---: | ---: |
| `encoding/json` | 756 (2) | 7,781 (2) | 323,068 (1,112) |
| `goccy/go-json` | 471 (2) | 3,198 (2) | 166,699 (2) |
| `json-iterator/go` | 692 (2) | 9,266 (2) | 379,963 (1,322) |
| `sonic` (`ConfigStd`) | 388 (3) | 3,187 (3) | 118,764 (3) |
| `jjs-go` | 768 (3) | 8,899 (3) | 476,436 (107) |
| `easyjson` | 704 (6) | 8,583 (67) | 349,747 (2,547) |

Sonic and goccy lead typed encoding on this fixture set. `jjs-go` uses fewer
allocations on Large, but its current helper-based value encoding is slower.

## Binding decoder

| Library | Small | Medium | Large |
| --- | ---: | ---: | ---: |
| `encoding/json` | 3,650 (15) | 65,941 (132) | 2,842,025 (4,903) |
| `goccy/go-json` | 353 (0) | 7,168 (1) | 378,820 (444) |
| `json-iterator/go` | 897 (18) | 19,397 (357) | 1,025,328 (18,715) |
| `sonic` (`ConfigStd`) | 721 (9) | 13,034 (109) | 563,937 (4,100) |
| `jjs-go` | 1,453 (12) | 21,399 (128) | 1,131,935 (5,139) |
| `easyjson` | 1,036 (7) | 17,427 (124) | 1,310,035 (4,338) |

The direct decoder beats `encoding/json` and EasyJSON on Large, but goccy and
Sonic remain substantially faster. Medium decode still trails goccy, Sonic,
EasyJSON and jsoniter.

## Generic encoder

EasyJSON is absent because generic values have no generated concrete type.

| Library | Small | Medium | Large |
| --- | ---: | ---: | ---: |
| `encoding/json` | 3,737 (52) | 55,912 (653) | 2,531,345 (27,955) |
| `goccy/go-json` | 1,486 (1) | 22,550 (1) | 1,127,414 (1) |
| `json-iterator/go` | 4,200 (61) | 64,222 (575) | 2,731,934 (22,566) |
| `sonic` (`ConfigStd`) | 1,219 (2) | 18,610 (2) | 876,434 (3) |
| `jjs-go` | 2,893 (7) | 35,027 (40) | 1,676,171 (1,267) |

## Generic decoder

| Library | Small | Medium | Large |
| --- | ---: | ---: | ---: |
| `encoding/json` | 3,984 (90) | 77,938 (771) | 3,589,965 (31,261) |
| `goccy/go-json` | 1,614 (21) | 20,138 (118) | 956,008 (3,526) |
| `json-iterator/go` | 3,619 (111) | 53,141 (1,075) | 2,296,966 (45,198) |
| `sonic` (`ConfigStd`) | 2,487 (72) | 39,182 (724) | 1,553,549 (29,781) |
| `jjs-go` | 2,988 (70) | 47,120 (728) | 2,220,880 (30,771) |

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

Direct-decoder types skip assignment-program generation. Unsupported types
still compile the compatibility JIT path.
