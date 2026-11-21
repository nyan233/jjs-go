# Go runtime loader compatibility: 1.21 through 1.27

Research date: 2026-11-21. The current stable toolchain is Go 1.27.4 according
to the official [Go downloads page](https://go.dev/dl/).

## Sources

The implementation was checked against the runtime source at these release
tags:

- [Go 1.21.13 `runtime/symtab.go`](https://go.googlesource.com/go/+/refs/tags/go1.21.13/src/runtime/symtab.go)
- [Go 1.23.12 `runtime/symtab.go`](https://go.googlesource.com/go/+/refs/tags/go1.23.12/src/runtime/symtab.go)
- [Go 1.24.0 `runtime/symtab.go`](https://go.googlesource.com/go/+/refs/tags/go1.24.0/src/runtime/symtab.go)
- [Go 1.26.11 `runtime/symtab.go`](https://go.googlesource.com/go/+/refs/tags/go1.26.11/src/runtime/symtab.go)
- [Go 1.27.4 `runtime/symtab.go`](https://go.googlesource.com/go/+/refs/tags/go1.27.4/src/runtime/symtab.go)
- `runtime/runtime2.go`, `runtime/mbarrier.go`, and `internal/abi/symtab.go`
  at the same tags.

The Sonic loader implementations `v0.5.3` and `v0.5.2-dev.arm64.0` were used as
secondary implementation references. Every layout used by `jjs-go` was checked
against the Go runtime source above.

## Loader primitives

A dynamically loaded function needs more than executable bytes:

1. Runtime `_func` metadata located by PC offsets.
2. A sorted `functab` and `findfunctab`.
3. pcdata tables for stack pointer delta and unsafe-point state.
4. funcdata pointer maps for arguments and locals.
5. A `moduledata` appended to the runtime's module chain.
6. Executable text memory and, on ARM64, instruction-cache invalidation.

The JIT function must remain discoverable while a Go helper can trigger GC.
Skipping module registration would make stack scanning stop at an unknown PC or
misinterpret the generated frame.

## Version matrix

| Go | Relevant change | Loader strategy |
| --- | --- | --- |
| 1.21 | Go 1.20 module layout with `covctrs` | 1.21 `moduledata`, `lastmoduledatap` linkname |
| 1.22 | Same module family as 1.21 | Shares the 1.21 layout |
| 1.23 | Module fields reorder `bad` before GC masks | Dedicated 1.23 layout |
| 1.24 | Linker begins enforcing private-linkname restrictions | Assembly call to `runtime.addmoduledata` |
| 1.25 | Same module and registration constraint as 1.24 | Shares the 1.24 path |
| 1.26 | `moduledata` adds `epclntab` | `lastmoduledatap` is marked for linkname again |
| 1.27 | `moduledata` adds `typedesclen`, `itaboffset`, `itabsize` | Dedicated 1.27 layout |

### Go 1.24 and 1.25 registration

Go 1.24's runtime declares `lastmoduledatap` without a public linkname marker,
and its `addmoduledata` comment says it is called from linker-generated
`.init_array`. A normal Go `//go:linkname` reference is rejected at link time.

The loader therefore has architecture assembly shims:

- amd64 places the new module pointer in `DI`, then calls
  `runtime·addmoduledata`.
- arm64 places it in `R0`, then calls the same runtime entry.

This uses the same path as dynamically linked modules and is active only for
Go 1.24 and 1.25.

### Go 1.26

The runtime now exposes `lastmoduledatap` and `moduledataverify1` for
linkname-based module loaders. It also adds the `epclntab` field to
`moduledata`. The loader returns to the module-chain linkname path and retains
the `epclntab` field.

### Go 1.27

The 1.27 `moduledata` adds:

```go
types, typedesclen, etypes uintptr
itaboffset, itabsize       uintptr
```

The older `typelinks` and `itablinks` fields are no longer in this layout.
`pcHeader` still reserves the old `textStart` word as an unused `uintptr`, so
the offsets after it remain unchanged.

The pclntab magic remains `0xfffffff1`; `functab` remains two `uint32` fields
and `_func` remains the Go 1.26 layout. The loader therefore reuses the pclntab
generator but uses a dedicated Go 1.27 `moduledata`.

## Other runtime-private calls

- `runtime.mallocgc(size, type, needzero)` has the same signature across the
  supported window.
- `runtime.makeslice`, `runtime.growslice`, `runtime.makemap`, and
  `runtime.mapassign` retain compatible signatures.
- Pointer-field clearing uses `reflect.typedmemclr`, which remains exposed by
  the runtime across the supported versions. The old `runtime.typedmemclr`
  target stops linking in Go 1.26.

## Verified toolchains

The loader's standalone load-and-execute test and the compiler JIT tests pass
with:

```text
go1.21.13
go1.22.0
go1.23.12
go1.24.0
go1.25.0
go1.26.11
go1.27.4
```

Command:

```sh
go test ./internal/jitloader ./core/compiler
```

This compatibility claim covers the loader and generated-function execution.
The repository's broader codec differential suite exposes separate Go 1.26
reflect/interface behavior that is outside the loader boundary; it remains on
the existing Go 1.22 primary verification target until that ABI work is done.

## Portability boundary

The loader is intentionally narrow. It does not expose the Sonic ABI wrapper or
instruction assembler APIs. Runtime-private layouts can change again; adding a
new Go minor release requires comparing `moduledata`, `functab`, `_func`,
pcHeader, and linkname policy before widening the build tags.
