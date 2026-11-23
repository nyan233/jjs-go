# Code generation architecture

The JIT frontend is shared across architectures. It lowers Go types into calls
to fixed encoder intrinsics plus stores for constant JSON fragments. An
architecture backend owns instruction selection, register mapping, stack-frame
layout, and machine-code emission.

## amd64 backend

- Uses `golang-asm/obj/x86` and the repository-owned `internal/jitloader`.
- Uses a fixed 64-byte Go ABI frame for helper calls and GC metadata.
- Lowers shared SSA into x86 assembler instructions, then applies the
  linear-scan allocator to surviving virtual scratch registers.
- Requires AVX2 and SSE4.2. Initialization fails fast on unsupported CPUs.
- Uses AVX2 for long string runs and SSE4.2 for short encoder strings.

## arm64 backend

- Targets `darwin/arm64`, including Apple M-series CPUs.
- Lowers shared SSA directly into ARM64 instructions.
- Uses register arguments from the Go internal ABI and an 80-byte ARM64 frame.
- Saves LR and FP, spills the encode context into a GC-visible frame slot, and
  calls the same architecture-neutral encoder intrinsics used by amd64.
- Encodes MOVZ/MOVK, MOV, LDR/STR, ADD/SUB, EOR, BLR, and RET directly.
- Invalidates data and instruction caches with `DC CVAU`, `IC IVAU`, `DSB`, and
  `ISB` before returning freshly loaded machine code.
- Uses word scanning for short strings and NEON after a short-prefix check
  for long encoder strings; the direct decoder retains word scanning.
- The direct decoder remains the primary decoder backend. The shared legacy
  decoder code generator can target the arm64 builder when a type cannot use a
  direct decode plan.

Both backends reserve helper argument spill slots separately from the saved
context. amd64 keeps the context at SP+48 and the saved BP at SP+56. ARM64
reserves SP+8 through SP+48 for up to six helper arguments, keeps the context
at SP+64, and follows Go's FP convention at SP-8. Local GC maps cover the
context slot relative to the runtime's `varp`; PC-to-SP tables describe the
prologue and epilogue as well as the body. Incoming register spill slots are
not marked as initialized stack arguments.

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
[`internal/jitloader/MODULEDATA.md`](internal/jitloader/MODULEDATA.md).

## Shared SSA optimizer

`core/compiler/jit_ssa.go` is the optimization boundary between the type
frontend and both backends. Every register definition receives a distinct
value ID. Stores, literal copies, calls and frame operations define memory
versions; loads refer to the memory version they read. All six helper argument
registers remain conservative implicit call operands, and calls kill available
register values. The pinned frame operations preserve the context root and
runtime PC-to-SP metadata.

The pass folds 64-bit constants (including wrapping arithmetic), propagates
copies, reuses available equal expressions/loads, and removes unused values
with a reverse liveness walk. Equal values are indexed by value number;
propagation checks that their physical homes still contain them. It cannot
reuse a register whose value has been overwritten. Memory versions invalidate
load reuse after every write or call, without attempting alias analysis.

The current generated functions are straight-line helper-call sequences, so
this is deliberately a single-basic-block SSA without phi nodes, branch/loop
optimization, scheduling, or register spilling. Helper control flow remains in
Go. Before moving branches into generated code, extend the representation with
basic blocks, dominance/phi construction and call-aware liveness. ABI homes
remain fixed; amd64 still allocates surviving virtual scratch intervals, while
ARM64 uses its existing fixed scratch registers. This is not a general-purpose
SSA register allocator.

`CodegenStats` includes input/effect nodes in its SSA counts. Instruction counts
refer to lowered backend operations (ARM64 instruction words; x86 assembler
instructions), and `CodeBytes` includes backend padding. Before/after counts
are within this pipeline; compare actual baseline binaries when evaluating a
previous compiler revision. The diagnostic before count performs a second
lowering without assembly or executable allocation. Cold code-generation
benchmarks include that cost. The primary direct decoder is Go code and does
not pass through SSA.

See [the M1 Pro evaluation](benchmark/compare/SSA_CODEGEN_ARM64.md) for the
measured tradeoff. Regression tests cover memory barriers, overwritten register
homes, implicit call operands, wrapping constants, random straight-line
programs, and stack growth/GC while a generated frame is active.

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

Native runtime tests and performance measurements run on an Apple M1 Pro.
CI additionally covers macOS, Windows/Linux amd64, ARM64 assembly/cross-builds
and Linux ARM64 under QEMU.
