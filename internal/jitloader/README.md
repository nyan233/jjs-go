# jitloader

`jitloader` is the repository-owned runtime loader for generated machine code.
Its `moduledata`, `functab`, `_func`, pcHeader, and stack-map layouts are
defined directly from the Go 1.21 through Go 1.27 runtime source.

The package only implements the loader surface needed by `jjs-go`:

- executable memory allocation,
- runtime function and module metadata,
- pcdata/funcdata and GC stack maps,
- runtime module-chain registration.

The loader has no Sonic source, imported ABI wrapper, instruction assembler, or
unrelated public API.

Version-specific behavior:

- Go 1.21-1.22: pre-1.23 module layout and `lastmoduledatap` linkname.
- Go 1.23: 1.23 module layout and linkname registration.
- Go 1.24-1.25: assembly registration through `runtime.addmoduledata`, because
  the Go linker rejects the private linkname.
- Go 1.26: adds `epclntab`.
- Go 1.27: adds `typedesclen`, `itaboffset`, and `itabsize` to `moduledata`.

The source-level compatibility matrix is in
[`docs/go-runtime-loader-versions.md`](../../docs/go-runtime-loader-versions.md).
The detailed `moduledata` organization is in
[`docs/moduledata-organization.md`](../../docs/moduledata-organization.md).
