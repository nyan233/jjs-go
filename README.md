# jjs-go

`jjs-go` is a type-specialized JSON codec that generates native x86-64 code.

`Marshal` reads struct layout and JSON tags at compile time, then generates a
cached function that loads each field by offset and calls fixed JIT intrinsics
for strings, booleans, integers, floats and JSON punctuation.

The compiler includes a linear-scan register allocator for non-reserved
physical registers. ABI registers used by runtime bridges remain fixed and are
not considered allocatable.

`Unmarshal` parses the input into a non-reflective JSON AST, then executes a
generated assignment function that writes fields by their compiled offsets.
There is no reflection-based encoding plan or `encoding/json` runtime fallback.

Current JIT type support:

- structs, nested structs, arrays and slices
- pointer fields and pointer roots
- maps through Go runtime `mapiterinit`/`mapiternext` and map allocation bridges
  (decoding currently covers string-keyed maps)
- interfaces through runtime type/interface metadata and dynamically compiled JIT units
- strings and JSON escape decoding
- booleans, signed/unsigned integers and floats
- JSON tags and ignored fields

Embedded fields and `omitempty` currently return an explicit unsupported-type
error. They are not silently routed through a reflection fallback.

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
- Windows amd64 or Linux amd64

Windows is the primary verification target for this branch. The experimental
assembly packages remain amd64-specific.

The project does not claim a performance win until the generated path has
passed broader differential and CPU-feature coverage.

## Verification

```sh
go test ./...
go test -race ./...
```
