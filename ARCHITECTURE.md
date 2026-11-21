# Code generation architecture

The JIT frontend is shared across architectures. It lowers Go types into calls
to fixed encoder intrinsics plus stores for constant JSON fragments. An
architecture backend owns instruction selection, register mapping, stack-frame
layout, and machine-code emission.

## amd64 backend

- Uses `golang-asm/obj/x86` and the repository-owned `internal/jitloader`.
- Uses a fixed 64-byte Go ABI frame for helper calls and GC metadata.
- Runs the x86 IR simplifier and linear-scan register allocator before
  assembling.
- Requires AVX2 and SSE4.2. Initialization fails fast on unsupported CPUs.
- Uses AVX2 for long string runs and SSE4.2 for short encoder strings.

## arm64 backend

- Targets `darwin/arm64`, including Apple M-series CPUs.
- Emits ARM64 instructions directly instead of reusing the x86 object model.
- Uses register arguments from the Go internal ABI and a 64-byte ARM64 frame.
- Saves LR and FP, spills the encode context into a GC-visible frame slot, and
  calls the same architecture-neutral encoder intrinsics used by amd64.
- Encodes MOVZ/MOVK, MOV, LDR/STR, ADD/SUB, EOR, BLR, and RET directly.
- Invalidates data and instruction caches with `DC CVAU`, `IC IVAU`, `DSB`, and
  `ISB` before returning freshly loaded machine code.
- Uses a WORD-at-a-time scanner for arm64 JSON string escaping.
- The direct decoder remains the primary decoder backend. The shared legacy
  decoder code generator can target the arm64 builder when a type cannot use a
  direct decode plan.

## Runtime loader

The repository owns its loader in `internal/jitloader`; it does not use the
Sonic loader at runtime. The loader generates the runtime `moduledata`,
`functab`, pcdata, funcdata, stack maps, and executable text mapping for one
loaded JIT function.

The runtime-private layout changes are separated across Go release families:

| Go | Module metadata | Registration |
| --- | --- | --- |
| 1.21-1.22 | `pclntab`, pre-1.23 module layout | `runtime.lastmoduledatap` |
| 1.23-1.25 | 1.23 module layout | linkname for 1.23; assembly `runtime.addmoduledata` for 1.24-1.25 |
| 1.26 | adds `epclntab` | `runtime.lastmoduledatap` |
| 1.27 | adds `typedesclen`, `itaboffset`, and `itabsize` | `runtime.lastmoduledatap` |

Go 1.24 tightened `//go:linkname` validation. The loader's 1.24 and 1.25
registration path therefore uses a small architecture assembly shim to call
the runtime linker entry point directly; Go 1.26 and later expose the needed
module-chain linkname again.

The source-level comparison and runtime layout matrix are recorded in
[`docs/go-runtime-loader-versions.md`](docs/go-runtime-loader-versions.md).
The field-by-field organization and lookup flow are documented in
[`docs/moduledata-organization.md`](docs/moduledata-organization.md).

The x86 IR optimizer and linear-scan allocator are currently compiled only on
amd64. The arm64 frontend reserves fixed scalar registers for context, data,
scratch values, and emitted literal-copy temporaries while helper calls cross
the Go ABI boundary.

## Tests and build checks

The native amd64 suite:

```sh
go test ./...
```

The arm64 instruction-generation tests can run on any Go host because they
assemble ARM64 instructions without executing them:

```sh
go test -tags arm64_asm_test ./core/compiler -run '^TestARM64'
```

Apple Silicon cross-builds:

```sh
GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go test -c ./core/compiler
GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go test -c .
```

On an x86 Linux host, user-mode QEMU executes the full ARM64 test binaries:

```sh
scripts/test-linux-arm64-qemu.sh
```

This exercises ARM64 register state, stack frames, helper calls, executable
memory, generated instructions, and JSON behavior. It does not test macOS JIT
entitlements or Darwin-specific executable-memory policy.

## macOS JIT notes

Runtime JIT loading uses the repository's own loader. A normal non-hardened
development binary may allocate writable memory and change it to executable
memory. Hardened or sandboxed macOS applications must grant the appropriate
executable-memory entitlement in their code signature.

The arm64 backend has been cross-compiled and its generated instruction
sequences have been validated on the current Windows host. A real Apple
Silicon CI/device run remains required before calling its runtime performance
production-verified.
