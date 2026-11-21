# Windows amd64 benchmark results

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
    -benchmem -benchtime 1s -count 3
```

Each table cell is the median of three samples in the form
`ns/op (allocs/op)`. Raw data is in
[`results-windows-amd64-final.txt`](results-windows-amd64-final.txt).

## Binding encoder

| Library | Small | Medium | Large |
| --- | ---: | ---: | ---: |
| `encoding/json` | 1,138 (2) | 7,616 (2) | 331,717 (1,112) |
| `goccy/go-json` | 642 (2) | 3,145 (2) | 169,234 (2) |
| `json-iterator/go` | 684 (2) | 8,486 (2) | 392,372 (1,322) |
| Sonic (`ConfigStd`) | 396 (3) | 3,193 (3) | 126,150 (3) |
| EasyJSON | 701 (6) | 8,607 (67) | 377,694 (2,547) |
| `jjs-go` | 762 (3) | 5,789 (3) | 377,902 (107) |

Typed jjs-go encoding beats stdlib on Small and Medium. On Large it is slightly
slower than stdlib and remains behind goccy and Sonic.

## Binding decoder

| Library | Small | Medium | Large |
| --- | ---: | ---: | ---: |
| `encoding/json` | 3,608 (15) | 65,729 (132) | 2,862,507 (4,903) |
| `goccy/go-json` | 350 (0) | 7,050 (1) | 381,700 (444) |
| `json-iterator/go` | 886 (18) | 18,667 (357) | 978,812 (18,715) |
| Sonic (`ConfigStd`) | 698 (9) | 13,711 (109) | 510,542 (4,100) |
| EasyJSON | 1,027 (7) | 17,162 (124) | 1,323,084 (4,338) |
| `jjs-go` | 1,303 (7) | 20,204 (107) | 1,097,236 (5,142) |

The direct decoder beats stdlib on every typed size. Large is 2.61x faster than
stdlib but still trails jsoniter, Sonic and goccy.

## Generic encoder

EasyJSON is absent because generic values have no generated concrete type.

| Library | Small | Medium | Large |
| --- | ---: | ---: | ---: |
| `encoding/json` | 3,707 (52) | 55,827 (653) | 2,462,122 (27,955) |
| `goccy/go-json` | 1,418 (1) | 22,051 (1) | 1,104,454 (1) |
| `json-iterator/go` | 4,056 (61) | 61,998 (575) | 2,612,538 (22,565) |
| Sonic (`ConfigStd`) | 1,186 (2) | 18,314 (2) | 857,194 (3) |
| `jjs-go` | 2,850 (7) | 24,338 (40) | 1,351,242 (1,267) |

## Generic decoder

| Library | Small | Medium | Large |
| --- | ---: | ---: | ---: |
| `encoding/json` | 3,935 (90) | 79,561 (771) | 3,590,047 (31,261) |
| `goccy/go-json` | 1,612 (21) | 20,566 (118) | 939,846 (3,526) |
| `json-iterator/go` | 3,535 (111) | 53,030 (1,075) | 2,318,649 (45,198) |
| Sonic (`ConfigStd`) | 2,394 (72) | 38,046 (724) | 1,556,728 (29,781) |
| `jjs-go` | 2,991 (70) | 47,407 (724) | 2,220,079 (30,009) |

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
