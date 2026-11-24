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
amd64 and ARM64 both use fused quote kernels that combine byte classification,
copying, escaping and validation in one bounded assembly call. amd64 handles
8/16/32-byte AVX2 blocks and scalar UTF-8 range validation. ARM64 handles
8/16-byte NEON blocks and validates non-ASCII blocks with three UTF-8 lookup
tables. Scalar instructions handle escapes, invalid sequences and short tails.
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
- standard-library-compatible map keys: strings, all integer widths,
  `encoding.TextMarshaler`/`TextUnmarshaler` keys, plus float keys on Go 1.27+
- map keys sorted by their formatted string, matching `encoding/json`

Unsupported map keys such as bool return a compile-time error from the JIT
plan instead of silently falling back to reflection. Map-key method precedence
and float-key support follow the host Go release because Go 1.27 enables the
jsonv2 semantics by default.

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
against `encoding/json`, `encoding/json/v2`, goccy/go-json, json-iterator,
EasyJSON and Sonic using Sonic's small/medium/large fixtures. Windows tables
are medians of three 500 ms runs; the ARM64 tables are medians of six
one-second samples from one full suite run. Each cell is
`ns/op (allocs/op)`; lower is better.

### Windows amd64 — Intel Core i7-14700KF

Measured on 2026-11-25 with Windows 11 26200, Go 1.25.0, an Intel Core
i7-14700KF, and `GOMAXPROCS=16`. Sonic uses `ConfigStd` and
`SONIC_NO_ASYNC_GC=1`. `encoding/json/v2` is enabled with
`GOEXPERIMENT=jsonv2` and uses options for nil map/slice, case-insensitive
names and invalid UTF-8 that keep the fixture semantics comparable to v1.
EasyJSON is absent from Generic because the generic fixtures have no generated
concrete type. Encoder samples use standard-compatible map ordering.

```powershell
cd benchmark/compare
$env:GOTOOLCHAIN='go1.25.0'
$env:GOEXPERIMENT='jsonv2'
$env:SONIC_NO_ASYNC_GC='1'
go test -run '^$' -bench '^(BenchmarkEncoder_|BenchmarkDecoder_)' `
  -benchmem -benchtime 500ms -count 3
```

Binding encoder:

| Library | Small | Medium | Large |
| --- | ---: | ---: | ---: |
| `encoding/json` | 1,264 (3) | 12,113 (5) | 429,588 (468) |
| `encoding/json/v2` | 1,323 (3) | 12,859 (5) | 452,773 (468) |
| `goccy/go-json` | 499.3 (2) | 3,503 (2) | 191,976 (2) |
| `json-iterator/go` | 703.8 (2) | 9,110 (2) | 427,243 (1,426) |
| Sonic (`ConfigStd`) | 434.0 (3) | 3,551 (3) | 145,878 (108) |
| EasyJSON | 766.6 (6) | 10,139 (69) | 464,955 (3,116) |
| **jjs-go** | **417.6 (2)** | **3,424 (2)** | **224,933 (2)** |

Binding decoder:

| Library | Small | Medium | Large |
| --- | ---: | ---: | ---: |
| `encoding/json` | 2,137 (0) | 28,132 (42) | 1,537,876 (4,419) |
| `encoding/json/v2` | 1,692 (0) | 22,087 (64) | 1,094,153 (3,336) |
| `goccy/go-json` | 354.4 (0) | 7,908 (1) | 383,788 (444) |
| `json-iterator/go` | 893.8 (18) | 19,213 (351) | 1,012,733 (17,681) |
| Sonic (`ConfigStd`) | 695.2 (9) | 11,561 (109) | 546,878 (4,100) |
| EasyJSON | 1,028 (7) | 17,074 (124) | 1,004,336 (4,338) |
| **jjs-go** | **850.4 (0)** | **14,358 (1)** | **702,576 (444)** |

Fresh-destination binding decoder:

| Library | Small | Medium | Large |
| --- | ---: | ---: | ---: |
| `encoding/json` | 2,636 (11) | 30,518 (70) | 1,596,443 (4,589) |
| `encoding/json/v2` | 2,101 (11) | 22,870 (68) | 1,132,883 (3,345) |
| `goccy/go-json` | 633.9 (6) | 9,389 (19) | 406,406 (583) |
| `json-iterator/go` | 1,220 (31) | 21,080 (379) | 1,072,531 (17,851) |
| Sonic (`ConfigStd`) | 1,043 (17) | 18,140 (128) | 667,420 (4,245) |
| EasyJSON | 1,255 (14) | 18,141 (128) | 1,038,458 (4,347) |
| **jjs-go** | **1,006 (6)** | **15,228 (19)** | **746,896 (587)** |

Generic encoder:

| Library | Small | Medium | Large |
| --- | ---: | ---: | ---: |
| `encoding/json` | 3,401 (26) | 34,405 (8) | 1,490,976 (9) |
| `encoding/json/v2` | 3,195 (15) | 28,704 (6) | 1,341,166 (6) |
| `goccy/go-json` | 1,465 (1) | 21,272 (1) | 1,119,223 (1) |
| `json-iterator/go` | 4,576 (66) | 70,094 (613) | 2,724,759 (23,832) |
| Sonic (`ConfigStd`) | 1,645 (7) | 21,979 (40) | 946,197 (1,267) |
| **jjs-go** | **2,040 (1)** | **30,908 (1)** | **1,474,881 (1)** |

Generic decoder:

| Library | Small | Medium | Large |
| --- | ---: | ---: | ---: |
| `encoding/json` | 7,393 (102) | 85,712 (930) | 3,943,236 (38,275) |
| `encoding/json/v2` | 3,801 (71) | 53,230 (678) | 2,514,917 (28,099) |
| `goccy/go-json` | 1,733 (23) | 19,915 (134) | 882,685 (4,087) |
| `json-iterator/go` | 3,332 (113) | 48,356 (1,084) | 2,129,693 (44,976) |
| Sonic (`ConfigStd`) | 2,583 (74) | 37,785 (747) | 1,491,083 (30,645) |
| **jjs-go** | **2,274 (23)** | **28,200 (134)** | **1,290,178 (4,083)** |

Unordered Generic encoder:

| Library | Small | Medium | Large |
| --- | ---: | ---: | ---: |
| `encoding/json/v2` | 3,212 (15) | 29,674 (6) | 1,383,649 (6) |
| `goccy/go-json` | 1,242 (1) | 12,093 (1) | 707,808 (1) |
| Sonic (unsorted, HTML escaping enabled) | 1,262 (7) | 13,034 (40) | 674,897 (1,267) |

Typed jjs-go encoding is fastest on Small and Medium and beats v1/v2,
json-iterator and EasyJSON on Large; Sonic and goccy remain ahead on Large.
Typed jjs-go decoding beats v1, v2, json-iterator and EasyJSON at every size;
goccy and Sonic remain ahead.

With standard-compatible map sorting, Generic jjs-go encoding remains faster
than v1 and json-iterator, but trails goccy, Sonic and jsonv2. Generic jjs-go
decoding has the lowest Medium latency and trails only goccy at all sizes.
jjs-go is intentionally absent from the unsorted-map control because its
default behavior now matches the standard library.

See [`benchmark/compare/README.md`](benchmark/compare/README.md) for the full
methodology. The tables above are the refreshed standard-map-order run from
2026-11-25; detailed raw files from the earlier run remain in
[`benchmark/compare/results-windows-amd64-quote-jsonv2.txt`](benchmark/compare/results-windows-amd64-quote-jsonv2.txt)
and
[`benchmark/compare/results-windows-amd64-jsonv2.txt`](benchmark/compare/results-windows-amd64-jsonv2.txt).

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
The fused AVX2 quote study is in
[`benchmark/compare/AMD64_QUOTE.md`](benchmark/compare/AMD64_QUOTE.md).

### macOS ARM64 — Apple M1 Pro (historical)

Measured on 2026-11-24 with macOS 27.0.1 (26A434), Go 1.25.0,
Apple M1 Pro (10 cores, 32 GiB RAM), and `GOMAXPROCS=10`, on AC power
with Low Power Mode disabled. The measured `main` revision is **`2813ca6`**,
after typed JIT encoder inlining, typed map encoders, NEON Base64 and the
earlier correctness, decoder, cache and ARM64 quoting/lookup4 changes.
The SSA experiment is excluded. Sonic uses `ConfigStd` with
`SONIC_NO_ASYNC_GC=1`, except for the explicitly unordered encoder below.
These samples predate standard-compatible map-key sorting. They are retained
as optimization history and must not be compared as current encoder results.

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

jjs-go's Binding encoding is 1.85–3.03x as fast as stdlib in these samples and
ahead of goccy, Sonic and EasyJSON at all three sizes. The typed JIT encoder
also removes most per-operation allocations: Binding Large reports 2 allocs/op,
down from 210 in the preceding run, and Generic encoding drops to one
allocation per operation.

jjs-go's Binding decoding is 4.58–5.25x as fast as stdlib in these samples and
ahead of Sonic and EasyJSON; goccy has a lower median at all three sizes.

These historical Generic encoder samples used unordered jjs-go map output.
Current jjs-go sorts keys like the standard library, so its current encoder
numbers are the Windows amd64 table above.

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
