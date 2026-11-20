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
    -benchmem -benchtime 1s -count 2
```

Each table cell is the mean of the two samples in the form
`ns/op (allocs/op)`. Lower is better. The raw runs are preserved in
[`results-windows-amd64.txt`](results-windows-amd64.txt).

## Binding encoder

| Library | Small | Medium | Large |
| --- | ---: | ---: | ---: |
| `encoding/json` | 1,997 (6) | 36,684 (68) | 1,119,531 (2,550) |
| `goccy/go-json` | 1,528 (6) | 21,571 (68) | 745,974 (2,549) |
| `json-iterator/go` | 808 (6) | 10,433 (68) | 439,334 (2,550) |
| `sonic` (`ConfigStd`) | 2,021 (7) | 36,077 (69) | 1,103,852 (2,551) |
| `jjs-go` | 854 (4) | 33,369 (9) | 6,595,996 (127) |
| `easyjson` | 710 (6) | 8,775 (67) | 388,728 (2,547) |

`jjs-go` is faster than the standard library on the Small binding encoder and
uses fewer allocations in all Binding encoder sizes. It becomes substantially
slower on Medium and Large, where nested dynamic `any` values reach runtime
map/interface helpers and generated unit dispatch.

## Binding decoder

| Library | Small | Medium | Large |
| --- | ---: | ---: | ---: |
| `encoding/json` | 2,821 (11) | 67,212 (130) | 3,697,069 (4,344) |
| `goccy/go-json` | 1,352 (8) | 24,745 (125) | 1,585,543 (4,340) |
| `json-iterator/go` | 2,093 (33) | 35,368 (448) | 2,183,163 (19,619) |
| `sonic` (`ConfigStd`) | 1,608 (9) | 24,689 (126) | 1,604,168 (4,341) |
| `jjs-go` | 29,732 (110) | 390,730 (987) | 29,658,045 (41,805) |
| `easyjson` | 1,035 (7) | 17,062 (124) | 1,296,262 (4,338) |

Decoding is currently the dominant weakness. The generated field writer is not
the bottleneck; parsing into the transient AST accounts for time and
allocations that scale with input size. Large Binding decode is 8.0x slower
than `encoding/json` and 22.9x slower than EasyJSON here.

## Generic encoder

EasyJSON is intentionally absent because it needs a generated concrete type.

| Library | Small | Medium | Large |
| --- | ---: | ---: | ---: |
| `encoding/json` | 3,692 (52) | 55,257 (653) | 2,495,827 (27,955) |
| `goccy/go-json` | 1,408 (1) | 22,120 (1) | 1,101,414 (1) |
| `json-iterator/go` | 4,024 (61) | 62,926 (575) | 2,663,986 (22,567) |
| `sonic` (`ConfigStd`) | 1,202 (2) | 18,330 (2) | 864,253 (3) |
| `jjs-go` | 2,862 (9) | 61,794 (50) | 13,329,552 (1,296) |

## Generic decoder

| Library | Small | Medium | Large |
| --- | ---: | ---: | ---: |
| `encoding/json` | 3,906 (90) | 78,229 (771) | 3,526,865 (31,262) |
| `goccy/go-json` | 1,563 (21) | 20,594 (118) | 940,951 (3,526) |
| `json-iterator/go` | 3,571 (111) | 52,396 (1,075) | 2,311,458 (45,198) |
| `sonic` (`ConfigStd`) | 2,460 (72) | 38,046 (724) | 1,560,169 (29,782) |
| `jjs-go` | 30,610 (143) | 398,492 (1,242) | 30,221,624 (50,886) |

## Interpretation

- Small binding encoding is competitive: `jjs-go` is 2.34x faster than the
  standard library, although EasyJSON and jsoniter are still faster.
- Large decoding needs a streaming typed decoder or a much cheaper AST. Merely
  optimizing field assignment will not close the current gap.
- Large Binding encoding uses only 127 allocations, but latency is high. The
  next target is dynamic map/interface encoding and helper dispatch, not
  buffer growth.
- Results are directional, not a universal ranking. CPU frequency, Windows
  scheduler behavior, sample shape, and codec configuration all affect them.
