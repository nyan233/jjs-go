# jitloader

`jitloader` is the repository-owned runtime loader for generated machine code.
It is an adapted derivative of the Apache-2.0 licensed Sonic loader, with
changes validated against the Go 1.21 through Go 1.27 runtime sources.

The package only implements the loader surface needed by `jjs-go`:

- executable memory allocation,
- runtime function and module metadata,
- pcdata/funcdata and GC stack maps,
- runtime module-chain registration.

The Sonic ABI wrappers, ARM64 instruction assembler, Tango-specific registration
path, and unrelated public APIs are intentionally omitted.

Version-specific behavior:

- Go 1.21-1.22: pre-1.23 module layout and `lastmoduledatap` linkname.
- Go 1.23: 1.23 module layout and linkname registration.
- Go 1.24-1.25: assembly registration through `runtime.addmoduledata`, because
  the Go linker rejects the private linkname.
- Go 1.26: adds `epclntab`.
- Go 1.27: adds `typedesclen`, `itaboffset`, and `itabsize` to `moduledata`.

The package license is in [LICENSE](LICENSE).
The source-level compatibility matrix is in
[`docs/go-runtime-loader-versions.md`](../../docs/go-runtime-loader-versions.md).
