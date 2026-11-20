# jjs-go

`jjs-go` is an experimental, type-specialized JSON codec written in Go.

The current default path compiles a cached encoding plan for each Go type. The
plan handles structs, pointers, strings, booleans, integers, floats, slices,
arrays, maps and `encoding/json` custom marshalers. Values that need full
standard-library behavior use `encoding/json` as a safe fallback.

Decoding validates the target once and caches a type-specific entry point while
using `encoding/json` for JSON parsing and assignment. This covers JSON tags,
custom unmarshalers, embedded fields and detailed syntax/type errors.

The earlier x86-64 JIT and executable-memory modules remain in the repository
as experimental low-level code. They are not on the public `Marshal`/`Unmarshal`
path because their bounds checking, runtime ABI compatibility and CPU feature
gating are not yet sufficient for general use. Their package initialization is
disabled unless `JJS_ENABLE_EXPERIMENTAL_JIT=1` is set.

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
	Email string `json:"email,omitempty"`
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

- Go 1.20 or newer
- Windows amd64 or Linux amd64

Windows is the primary verification target for this branch. The experimental
assembly packages remain amd64-specific.

The safe encoding-plan path is currently a correctness and architecture
baseline. It has not yet demonstrated a performance win over `encoding/json`;
that claim should only be made after the JIT path passes differential, bounds,
race and CPU-feature tests.

## Verification

```sh
go test ./...
go test -race ./...
```
