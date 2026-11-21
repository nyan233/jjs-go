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
`ns/op (allocs/op)`. Lower is better. The optimized raw run is preserved in
[`results-windows-amd64-ir-direct.txt`](results-windows-amd64-ir-direct.txt).
The first optimization pass is in
[`results-windows-amd64-optimized.txt`](results-windows-amd64-optimized.txt),
and the pre-optimization run is in
[`results-windows-amd64.txt`](results-windows-amd64.txt).

## jjs-go optimization pass

| Scenario | Before | After | Improvement |
| --- | ---: | ---: | ---: |
| Binding encode Medium | 33,369 ns | 9,749 ns | 3.42x |
| Binding encode Large | 6,595,996 ns | 449,654 ns | 14.67x |
| Binding decode Medium | 390,730 ns | 52,986 ns | 7.37x |
| Binding decode Large | 29,658,045 ns | 2,026,779 ns | 14.63x |
| Generic encode Large | 13,329,552 ns | 1,734,626 ns | 7.68x |
| Generic decode Large | 30,221,624 ns | 2,338,196 ns | 12.93x |

The compiler IR/direct-decoder pass produced another measurable step:

| Scenario | First optimization | IR + direct decoder | Additional improvement |
| --- | ---: | ---: | ---: |
| Binding encode Small | 850 ns | 779 ns | 1.09x |
| Binding encode Large | 449,654 ns | 439,923 ns | 1.02x |
| Binding decode Small | 4,290 ns | 1,465 ns | 2.93x |
| Binding decode Medium | 52,986 ns | 21,985 ns | 2.41x |
| Binding decode Large | 2,026,779 ns | 1,151,745 ns | 1.76x |
| Generic decode Small | 4,323 ns | 3,030 ns | 1.43x |
| Generic decode Medium | 52,781 ns | 47,360 ns | 1.11x |

Large Binding decode also dropped from 39,981 to 5,139 allocs/op. The decoder no
longer constructs a transient JSON AST for direct-plan types. It consumes the
input into the destination recursively, uses no-copy string views for field
lookups and skipped values, and parses numeric spans without temporary strings.
Types containing non-empty interfaces retain the earlier JIT path.

The IR reports 3.52% fewer encoder instructions on the nested test type after
peephole folding. Its 12 virtual register lifetimes are mapped to one physical
register, giving 11 reuses and zero spills.

For `TwitterResponse`, `go run ./cmd/codegenstats` reports:

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

Branch folding is implemented in the IR but reports zero removals because the
current encoder templates do not emit redundant branches. Direct-decoder types
skip assignment-program generation entirely; unsupported types still compile
the compatibility JIT path.

Large Binding encode also dropped from 1,194,738 B/op to 272,747 B/op. The
changes were:

- Decode unescaped strings directly from the input slice instead of first
  building a temporary byte buffer.
- Keep short and medium strings on a scalar path, only invoking AVX2 after a
  256-byte prefix.
- Use a decode-specific AVX2 scanner that ignores `<`, `>` and `&`.
- Stop re-entering AVX2 after entering a non-ASCII string; process the
  remainder scalar to avoid repeated AVX setup per Unicode rune.
- Learn the previous encoded size and use it as the next allocation capacity.
- Reserve string and literal capacity before appending, with geometric growth.

## Binding encoder

| Library | Small | Medium | Large |
| --- | ---: | ---: | ---: |
| `encoding/json` | 1,961 (6) | 36,128 (68) | 1,112,062 (2,550) |
| `goccy/go-json` | 1,504 (6) | 21,732 (68) | 732,144 (2,549) |
| `json-iterator/go` | 820 (6) | 10,246 (68) | 435,947 (2,550) |
| `sonic` (`ConfigStd`) | 2,015 (7) | 38,148 (69) | 1,093,096 (2,551) |
| `jjs-go` | 779 (3) | 10,063 (3) | 439,923 (107) |
| `easyjson` | 702 (6) | 8,835 (67) | 387,791 (2,547) |

`jjs-go` is faster than the standard library on the Small binding encoder and
uses fewer allocations in all Binding encoder sizes. On Large it is close to
the leading EasyJSON/jsoniter group while using fewer bytes and allocations.

## Binding decoder

| Library | Small | Medium | Large |
| --- | ---: | ---: | ---: |
| `encoding/json` | 2,847 (11) | 68,205 (130) | 3,738,463 (4,344) |
| `goccy/go-json` | 1,361 (8) | 25,414 (125) | 1,631,724 (4,341) |
| `json-iterator/go` | 2,092 (33) | 35,847 (448) | 2,248,028 (19,619) |
| `sonic` (`ConfigStd`) | 1,616 (9) | 25,454 (126) | 1,637,355 (4,342) |
| `jjs-go` | 1,465 (12) | 21,985 (128) | 1,151,745 (5,139) |
| `easyjson` | 1,033 (7) | 17,407 (124) | 1,325,133 (4,338) |

`jjs-go` is now faster than `encoding/json`, jsoniter and Sonic on Small
decoding, and has the fastest Large typed decode in this comparison. EasyJSON
still leads Medium decoding.

## Generic encoder

EasyJSON is intentionally absent because it needs a generated concrete type.

| Library | Small | Medium | Large |
| --- | ---: | ---: | ---: |
| `encoding/json` | 3,758 (52) | 56,667 (653) | 2,540,125 (27,955) |
| `goccy/go-json` | 1,435 (1) | 22,487 (1) | 1,115,504 (1) |
| `json-iterator/go` | 4,094 (61) | 63,342 (575) | 2,676,221 (22,567) |
| `sonic` (`ConfigStd`) | 1,241 (2) | 18,809 (2) | 885,763 (3) |
| `jjs-go` | 2,936 (7) | 35,031 (40) | 1,883,548 (1,267) |

## Generic decoder

| Library | Small | Medium | Large |
| --- | ---: | ---: | ---: |
| `encoding/json` | 3,978 (90) | 77,653 (771) | 3,548,659 (31,260) |
| `goccy/go-json` | 1,584 (21) | 20,313 (118) | 936,210 (3,526) |
| `json-iterator/go` | 3,613 (111) | 51,967 (1,075) | 2,321,559 (45,198) |
| `sonic` (`ConfigStd`) | 2,494 (72) | 38,758 (724) | 1,566,563 (29,782) |
| `jjs-go` | 3,030 (70) | 47,360 (728) | 2,297,048 (30,771) |

## Interpretation

- Small binding encoding is competitive: `jjs-go` is 2.52x faster than the
  standard library, although EasyJSON and jsoniter are still faster.
- Small, Medium and Large Binding decode now beats `encoding/json`. Large
  Binding decode is the fastest result in this fixture set.
- Large generic decode still trails goccy and Sonic because converting unknown
  JSON shapes into `map[string]any` and `[]any` requires runtime allocation.
- Generic encoding still trails Sonic and goccy because dynamic map/interface
  dispatch prevents the same specialization available to concrete structs.
- Results are directional, not a universal ranking. CPU frequency, Windows
  scheduler behavior, sample shape, and codec configuration all affect them.
