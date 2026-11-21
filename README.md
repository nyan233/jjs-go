# jjs-go

`jjs-go` is a type-specialized JSON codec that generates native x86-64 code.

`Marshal` reads struct layout and JSON tags at compile time, then generates a
cached function that loads each field by offset and calls fixed JIT intrinsics
for strings, booleans, integers, floats and JSON punctuation.

The compiler includes a linear-scan register allocator for non-reserved
physical registers. ABI registers used by runtime bridges remain fixed and are
not considered allocatable.

Encode and decode contexts are reused through `sync.Pool`. The generated code
reserves output capacity once per static segment and writes compiled literals
directly with 16/32-byte `VMOVDQU` stores instead of calling a copy helper.
Short and medium strings stay on a scalar path; AVX2 is used only where its
fixed setup cost is amortized. Unescaped decode strings are converted directly
from the input slice, and encoders use the previous output size as a capacity
hint to avoid repeated growth copies.

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

Current JIT type support:

- structs, nested structs, arrays and slices
- pointer fields and pointer roots
- maps through Go runtime `mapiterinit`/`mapiternext` and map allocation bridges
  (decoding currently covers string-keyed maps)
- interfaces through runtime type/interface metadata and dynamically compiled JIT units
- strings and JSON escape decoding
- booleans, signed/unsigned integers and floats
- JSON tags, `omitempty` and ignored fields
- promoted fields from embedded structs, including embedded pointers
- user-defined `json.Marshaler` and `json.Unmarshaler`, including named
  interface fields and nil pointer/interface values

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

## Requirements

- Go 1.22 or newer
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

The project does not claim a performance win until the generated path has
passed broader differential and CPU-feature coverage.

## Benchmarks

`benchmark/compare` is an independent Go module that compares this checkout
against `encoding/json`, goccy/go-json, json-iterator, EasyJSON and Sonic using
Sonic's small/medium/large fixtures. See
[`benchmark/compare/README.md`](benchmark/compare/README.md) for methodology
and [`benchmark/compare/RESULTS.md`](benchmark/compare/RESULTS.md) for the
latest Windows amd64 results.

## Verification

```sh
go test ./...
go test -race ./...
```
