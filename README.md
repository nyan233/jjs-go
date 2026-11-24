# jjs-go

`jjs-go` is a type-specialized JSON codec with native JIT backends for amd64
and ARM64. It provides `Marshal`/`Unmarshal`-style APIs without a reflection
encoder plan or an `encoding/json` runtime fallback.

`Marshal` reads struct layout and JSON tags once per concrete type, then caches
a generated function that loads each field by offset and calls fixed JIT intrinsics
for strings, booleans, integers, floats and JSON punctuation.

On amd64, the compiler includes a machine-instruction IR and a linear-scan
register allocator for non-reserved physical registers. ABI registers used by
runtime bridges remain fixed and are not considered allocatable. The ARM64
backend emits its own ABI-compatible prologue, helper calls, scalar operations
and epilogue directly.

Encode and decode contexts are reused through `sync.Pool`. The generated code
reserves output capacity once per static segment and writes compiled literals
directly with 16/32-byte `VMOVDQU` stores instead of calling a copy helper.
amd64 string scanning combines SSE4.2 for short encoder strings and AVX2 for
longer runs. ARM64 uses hand-written NEON quoting that fuses byte classification, copying,
escaping and UTF-8 validation in one bounded assembly call. NEON handles 8/16-byte
ASCII blocks and validates non-ASCII blocks with three UTF-8 lookup tables.
Scalar ARM64 instructions handle escapes, invalid sequences and short tails.
amd64 byte slices use a bounded AVX2 Base64 kernel with exact 12-byte vector
loads and a scalar tail; ARM64 uses an equivalent generated NEON kernel.
Decoding retains word scanning based on full-document measurements.
Unescaped decode strings are validated once, and short values are copied into
append-only storage chunks. Encoders use a
bounded previous-output size hint to avoid repeated growth copies, and
`MarshalTo` can reuse caller-owned output storage.

`Unmarshal` compiles a direct recursive-descent decode plan for supported
types. It parses fields directly into compiled offsets without constructing a
JSON AST. Field keys and skipped strings use input-backed views. Integer
validation and conversion share one pass, and ordered fields use a direct
match before falling back to name lookup. Types containing non-empty
interfaces retain the earlier JIT path. There is no reflection-based encoding
plan or `encoding/json` runtime fallback.

Generated encoder programs pass through a small machine-instruction IR before
assembly. Redundant moves and foldable immediate operations are removed, and
scratch values use virtual registers with live intervals rather than permanent
global scratch slots. `CodegenStats` reports instruction reduction, register
reuse, spills and generated code size.

The runtime loader is implemented in [`internal/jitloader`](internal/jitloader/README.md).
It generates Go `moduledata`, `functab`, `_func`, pcdata/funcdata and GC stack
maps directly from the runtime layouts for Go 1.21 through Go 1.27.4, without
importing the Sonic loader.

## Usage

```go
package main

import (
	"fmt"

	jjs "github.com/nyan233/jjs-go"
)

type User struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

func main() {
	input := User{ID: 7, Name: "Ada"}

	data, err := jjs.Marshal(input)
	if err != nil {
		panic(err)
	}

	var output User
	if err := jjs.Unmarshal(data, &output); err != nil {
		panic(err)
	}
	fmt.Printf("%s\n", data)
}
```

Reuse a buffer across calls with `MarshalTo`:

```go
buf := make([]byte, 0, 1024)
buf, err := jjs.MarshalTo(buf, &input)
```

This replaces the buffer's previous contents; always use the returned slice.
Consume or copy the result before reusing the buffer. The buffer must not
overlap the input's storage. Custom marshalers and growing collections may
still allocate.

## Supported types

- concrete structs, nested structs and pointer roots
- arrays, slices, maps and interfaces
- promoted fields from embedded structs and embedded pointers
- JSON tags, `omitempty` and `json:"-"`
- user-defined `json.Marshaler` and `json.Unmarshaler`
- nil pointers, nil maps/slices and typed-nil interfaces

`Unmarshal` currently supports string-keyed maps. Unsupported types return a
compile-time error from the JIT plan instead of silently falling back to
reflection.

## Requirements

- Go 1.21 or newer
- amd64 on Windows or Linux with AVX2 and SSE4.2; initialization fails fast
  otherwise
- arm64 on macOS (Apple M-series) through the native ARM64 JIT backend

The code-generation split and ARM64 validation commands are documented in
[`ARCHITECTURE.md`](ARCHITECTURE.md).

Windows amd64 and Apple M1 Pro ARM64 measurements are reported separately
below. Native ARM64 execution is also covered by macOS CI. String escaping
and JSON string parsing use AVX2 and SSE4.2 fast paths on amd64; ARM64 uses
native word scans for decoding and fused NEON quoting for encoding.

The repository-owned loader and compiler JIT tests are validated against Go
1.21 through Go 1.27.4. The broader codec differential suite still uses Go
1.22 as its primary target because Go 1.26 introduced additional
reflection/interface ABI differences outside the loader itself.

## Architecture

- [ARCHITECTURE.md](ARCHITECTURE.md): backend split, ABI, ARM64 and registration strategy.
- [internal/jitloader/MODULEDATA.md](internal/jitloader/MODULEDATA.md): runtime metadata and GC layout.
- [docs/go-runtime-loader-versions.md](docs/go-runtime-loader-versions.md): Go 1.21–1.27 source-level compatibility matrix.

The type frontend is shared between architectures. amd64 uses the x86
instruction builder, IR optimizer and linear-scan allocator. ARM64 uses a
direct instruction encoder and its own Go ABI frame/cache-flush sequence.

## Benchmarks

`benchmark/compare` is an independent Go module that compares this checkout
against `encoding/json`, goccy/go-json, json-iterator, EasyJSON and Sonic using
Sonic's small/medium/large fixtures. Windows tables are medians of three
one-second runs; the ARM64 tables are medians of six one-second samples from
one full suite run. Each cell is `ns/op (allocs/op)`; lower is better.

### Windows amd64 — Intel Core i7-14700KF

Measured on 2026-11-21 with Windows 11 26200, Go 1.22.0, an Intel Core
i7-14700KF, and `GOMAXPROCS=16`. Sonic uses `ConfigStd` and
`SONIC_NO_ASYNC_GC=1`. EasyJSON is absent from Generic because the generic
fixtures have no generated concrete type.

```powershell
cd benchmark/compare
$env:SONIC_NO_ASYNC_GC='1'
go test -run '^$' -bench '^(BenchmarkEncoder_|BenchmarkDecoder_)' `
  -benchmem -benchtime 1s -count 3
```

Binding encoder:

| Library | Small | Medium | Large |
| --- | ---: | ---: | ---: |
| `encoding/json` | 1,138 (2) | 7,616 (2) | 331,717 (1,112) |
| `goccy/go-json` | 642 (2) | 3,145 (2) | 169,234 (2) |
| `json-iterator/go` | 684 (2) | 8,486 (2) | 392,372 (1,322) |
| Sonic (`ConfigStd`) | 396 (3) | 3,193 (3) | 126,150 (3) |
| EasyJSON | 701 (6) | 8,607 (67) | 377,694 (2,547) |
| **jjs-go** | **762 (3)** | **5,789 (3)** | **377,902 (107)** |

Binding decoder:

| Library | Small | Medium | Large |
| --- | ---: | ---: | ---: |
| `encoding/json` | 3,608 (15) | 65,729 (132) | 2,862,507 (4,903) |
| `goccy/go-json` | 350 (0) | 7,050 (1) | 381,700 (444) |
| `json-iterator/go` | 886 (18) | 18,667 (357) | 978,812 (18,715) |
| Sonic (`ConfigStd`) | 698 (9) | 13,711 (109) | 510,542 (4,100) |
| EasyJSON | 1,027 (7) | 17,162 (124) | 1,323,084 (4,338) |
| **jjs-go** | **1,303 (7)** | **20,204 (107)** | **1,097,236 (5,142)** |

Generic encoder:

| Library | Small | Medium | Large |
| --- | ---: | ---: | ---: |
| `encoding/json` | 3,707 (52) | 55,827 (653) | 2,462,122 (27,955) |
| `goccy/go-json` | 1,418 (1) | 22,051 (1) | 1,104,454 (1) |
| `json-iterator/go` | 4,056 (61) | 61,998 (575) | 2,612,538 (22,565) |
| Sonic (`ConfigStd`) | 1,186 (2) | 18,314 (2) | 857,194 (3) |
| **jjs-go** | **2,850 (7)** | **24,338 (40)** | **1,351,242 (1,267)** |

Generic decoder:

| Library | Small | Medium | Large |
| --- | ---: | ---: | ---: |
| `encoding/json` | 3,935 (90) | 79,561 (771) | 3,590,047 (31,261) |
| `goccy/go-json` | 1,612 (21) | 20,566 (118) | 939,846 (3,526) |
| `json-iterator/go` | 3,535 (111) | 53,030 (1,075) | 2,318,649 (45,198) |
| Sonic (`ConfigStd`) | 2,394 (72) | 38,046 (724) | 1,556,728 (29,781) |
| **jjs-go** | **2,991 (70)** | **47,407 (724)** | **2,220,079 (30,009)** |

The direct decoder is substantially faster than `encoding/json` on all typed
sizes, including a 2.61x advantage on Large. Typed jjs-go encoding is faster
than the standard library on Small and Medium, but remains slower than goccy,
Sonic and the standard library on Large. Generic jjs-go performance is faster
than stdlib on all sizes, while goccy and Sonic remain ahead.

See [`benchmark/compare/README.md`](benchmark/compare/README.md) for the full
methodology and
[`benchmark/compare/results-windows-amd64-final.txt`](benchmark/compare/results-windows-amd64-final.txt)
for all three raw samples.

The subsequent hot-path changes are measured separately against correctness
commit `5b211eb` on Apple M1 Pro with Go 1.25.0. See
[`benchmark/compare/HOTPATH_OPTIMIZATION.md`](benchmark/compare/HOTPATH_OPTIMIZATION.md)
for paired before/after measurements, allocation counts and commands. These
ARM64 measurements do not replace the Windows amd64 results above.

Hardware-counter profiling notes and raw results are in
[`benchmark/compare/PROFILING.md`](benchmark/compare/PROFILING.md),
[`benchmark/compare/HARDWARE_RESULTS.md`](benchmark/compare/HARDWARE_RESULTS.md)
and [`benchmark/compare/SHORT_STRING_SCAN.md`](benchmark/compare/SHORT_STRING_SCAN.md).
The AVX2 byte-slice study is in
[`benchmark/compare/AMD64_BASE64.md`](benchmark/compare/AMD64_BASE64.md).

### macOS ARM64 — Apple M1 Pro

Measured on 2026-11-24 with macOS 27.0.1 (26A434), Go 1.25.0,
Apple M1 Pro (10 cores, 32 GiB RAM), and `GOMAXPROCS=10`, on AC power
with Low Power Mode disabled. The measured `main` revision is **`2813ca6`**,
after typed JIT encoder inlining, typed map encoders, NEON Base64 and the
earlier correctness, decoder, cache and ARM64 quoting/lookup4 changes.
The SSA experiment is excluded. Sonic uses `ConfigStd` with
`SONIC_NO_ASYNC_GC=1`, except for the explicitly unordered encoder below.

Values are medians of six one-second samples, shown as **ns/op (allocs/op)**.
All loops are serial and codecs are warmed before timing. Encoding includes
output allocation. Binding decode reuses a destination; FreshBinding creates
one per iteration; Generic decode resets its destination to nil.
Go reports integer average allocation counts, so a reported zero can coexist
with nonzero B/op when allocation is amortized across operations.
The machine remained in normal desktop use with all applications running.
The Telegram process, a sustained background CPU consumer of about 85%, was
moved to the Darwin background scheduling policy for the measurement and
restored afterward; two uncontrolled passes measured the unchanged libraries
about 16–20% slower on average. The Windows amd64 results use a different Go
version, codec
revision and configuration; compare libraries within each platform's table.

```sh
cd benchmark/compare
SONIC_NO_ASYNC_GC=1 GOMAXPROCS=10 go test -run '^$' \
  -bench '^(BenchmarkEncoder_|BenchmarkDecoder_)' \
  -benchmem -benchtime=1s -count=6 -cpu=10
```

Binding encoder:

| Library | Small | Medium | Large |
| --- | ---: | ---: | ---: |
| `encoding/json` | 897.5 (2) | 10,352 (2) | 410,798 (1,112) |
| `goccy/go-json` | 575.2 (2) | 4,050 (2) | 215,806 (2) |
| `json-iterator/go` | 1,022 (2) | 21,430 (2) | 644,206 (1,426) |
| Sonic (`ConfigStd`) | 1,594 (3) | 16,743 (3) | 485,286 (107) |
| EasyJSON | 914.2 (6) | 10,891 (67) | 414,778 (2,547) |
| **jjs-go** | **485.1 (2)** | **3,414 (2)** | **137,045 (2)** |

Binding decoder:

| Library | Small | Medium | Large |
| --- | ---: | ---: | ---: |
| `encoding/json` | 4,276 (15) | 88,942 (132) | 3,950,088 (4,903) |
| `goccy/go-json` | 434.6 (0) | 8,372 (1) | 441,072 (444) |
| `json-iterator/go` | 1,118 (18) | 26,636 (351) | 1,368,273 (17,681) |
| Sonic (`ConfigStd`) | 1,350 (10) | 22,048 (110) | 899,736 (4,099) |
| EasyJSON | 1,184 (7) | 19,318 (124) | 1,891,829 (4,338) |
| **jjs-go** | **933.3 (0)** | **16,932 (1)** | **852,557 (444)** |

Generic encoder:

| Library | Small | Medium | Large |
| --- | ---: | ---: | ---: |
| `encoding/json` | 4,122 (52) | 63,710 (653) | 2,833,784 (27,955) |
| `goccy/go-json` | 1,678 (1) | 24,935 (1) | 1,285,131 (1) |
| `json-iterator/go` | 5,110 (66) | 84,578 (613) | 3,440,172 (23,834) |
| Sonic (`ConfigStd`) | 4,250 (7) | 50,548 (40) | 2,244,160 (1,268) |
| **jjs-go** | **1,434 (1)** | **15,131 (1)** | **782,752 (1)** |

Generic decoder:

| Library | Small | Medium | Large |
| --- | ---: | ---: | ---: |
| `encoding/json` | 4,888 (92) | 105,178 (794) | 4,854,846 (32,124) |
| `goccy/go-json` | 2,124 (23) | 24,631 (134) | 1,089,802 (4,087) |
| `json-iterator/go` | 4,084 (113) | 63,955 (1,084) | 2,857,082 (44,976) |
| Sonic (`ConfigStd`) | 2,606 (73) | 36,843 (705) | 1,561,852 (28,731) |
| **jjs-go** | **2,633 (23)** | **35,194 (134)** | **1,535,946 (4,084)** |

Unordered Generic encoder:

| Library | Small | Medium | Large |
| --- | ---: | ---: | ---: |
| `goccy/go-json` | 1,441 (1) | 14,878 (1) | 846,360 (1) |
| Sonic (unsorted, HTML escaping enabled) | 4,034 (7) | 41,912 (40) | 1,907,963 (1,267) |
| **jjs-go** | **1,421 (1)** | **14,771 (1)** | **780,358 (1)** |

jjs-go's Binding encoding is 1.85–3.03x as fast as stdlib in these samples and
ahead of goccy, Sonic and EasyJSON at all three sizes. The typed JIT encoder
also removes most per-operation allocations: Binding Large reports 2 allocs/op,
down from 210 in the preceding run, and Generic encoding drops to one
allocation per operation.

jjs-go's Binding decoding is 4.58–5.25x as fast as stdlib in these samples and
ahead of Sonic and EasyJSON; goccy has a lower median at all three sizes.

Generic jjs-go encoding has the lowest median at all three sizes. Generic
jjs-go decoding is 1.86–3.16x as fast as stdlib and level with Sonic; goccy
remains ahead. In the unsorted-map control, jjs-go is ahead of goccy at every
size and well ahead of Sonic.

Default Generic encoding includes different map-order behavior: jjs-go does
not sort keys, while stdlib and Sonic `ConfigStd` do. The UnorderedGeneric
control disables map sorting for jjs-go, goccy and Sonic while retaining HTML
escaping and string validation. Use that control when assessing their generic
encoding performance with sorting removed.

The [full ARM64 report](benchmark/compare/RESULTS_ARM64.md) includes the
fresh-destination decode table, complete methodology, host controls and sample
variability. The [raw output](benchmark/compare/results-darwin-arm64-main-2813ca6.txt)
contains all 558 samples; all tables above come from this one run. The
[cb635eb report](benchmark/compare/RESULTS_ARM64_cb635eb.md) and the
[96ffa5b report](benchmark/compare/RESULTS_ARM64_96ffa5b.md) remain available
as historical snapshots.

The [decoder/SIMD study](benchmark/compare/CODEC_OPTIMIZATION_ARM64.md),
[assembly quoting study](benchmark/compare/APPEND_QUOTED_ARM64.md) and
[paper-guided UTF-8 follow-up](benchmark/compare/QUOTING_PAPERS_ARM64.md)
record the individual optimization stages with their own before/after baselines.

## Verification

```sh
go test ./...
go test -race ./...
```

Loader and compiler compatibility matrix:

```sh
go test ./internal/jitloader ./core/compiler
```

ARM64 instruction-generation tests:

```sh
go test -tags arm64_asm_test ./core/compiler -run '^TestARM64'
```

darwin/arm64 cross-build:

```sh
GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go test -c ./core/compiler
```

## Known limitations

- Go 1.26/1.27 loader and compiler JIT tests pass, but the full codec
  differential suite still exposes separate reflect/interface ABI behavior.
- ARM64 performance measurements cover Apple M1 Pro; other ARM64 hardware
  has not been benchmarked here.
- Hardened macOS apps need executable-memory/JIT entitlements in their code
  signature.

## Branches

- `main` tracks the current architecture, native ARM64 backend, custom runtime
  loader and profiling work.
- `zbh-dev` preserves the pre-rewrite `main` snapshot.
