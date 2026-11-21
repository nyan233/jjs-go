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
longer runs. ARM64 uses native 64-bit word scanning. Unescaped decode strings
are converted directly from the input slice, and encoders use the previous
output size as a capacity hint to avoid repeated growth copies.

`Unmarshal` compiles a direct recursive-descent decode plan for supported
types. It parses fields directly into compiled offsets without constructing a
JSON AST. Field keys and skipped strings use input-backed views, and numeric
spans are converted without temporary strings. Types containing non-empty
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

Windows amd64 is the primary performance-verification target for this branch.
The ARM64 backend is cross-compiled and instruction-tested, but still requires
a real Apple Silicon runtime/CI pass before its performance is considered
production-verified. String escaping and JSON string parsing use AVX2 and
SSE4.2 fast paths on amd64; ARM64 uses native 64-bit word scans.

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
Sonic's small/medium/large fixtures. The tables below are medians of three
one-second runs. Each cell is `ns/op (allocs/op)`; lower is better.

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

Hardware-counter profiling notes and raw results are in
[`benchmark/compare/PROFILING.md`](benchmark/compare/PROFILING.md),
[`benchmark/compare/HARDWARE_RESULTS.md`](benchmark/compare/HARDWARE_RESULTS.md)
and [`benchmark/compare/SHORT_STRING_SCAN.md`](benchmark/compare/SHORT_STRING_SCAN.md).

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
- ARM64 is cross-compiled and instruction-tested; real Apple Silicon runtime
  performance still needs CI/device validation.
- Hardened macOS apps need executable-memory/JIT entitlements in their code
  signature.

## Branches

- `main` tracks the current architecture, native ARM64 backend, custom runtime
  loader and profiling work.
- `zbh-dev` preserves the pre-rewrite `main` snapshot.
