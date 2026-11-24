# Sonic ARM64 development-branch source audit

Checked 2026-11-24 for the M1 Pro comparison. Exact Sonic source:
`ba8b7aa902f6d93014423b89a4290a3955f85d6d`, the observed tip of
`feat/dev_hw_arm`. Target toolchain: Go 1.25.0, darwin/arm64.
This note records source facts and a validation protocol. The final section
records local backend/correctness checks; measurements are in the
[comparison report](SONIC_ARM64_JIT_COMPARISON.md).

## Encoder selection and execution evidence

The ARM64 initializer calls `ForceUseJit()` unless `vars.UseVM` is true.
`ForceUseJit()` installs `arm64.EncodeTypedPointer`; compilation converts the
common type IR through `arm64.NewAssembler`, then loads machine code. This is
a native ARM64 encoder backend. Source:
[ARM64 pools](https://github.com/bytedance/sonic/blob/ba8b7aa902f6d93014423b89a4290a3955f85d6d/internal/encoder/pools_arm64.go).

`SONIC_ENCODER_USE_VM` being **nonempty** selects the VM. An unset variable
selects the default JIT on this branch; the string `0` still selects VM.
The public encoder package exports `GetUseVM`, `ForceUseVM` and `ForceUseJit`.
Record `GetUseVM()` before and after warmed fixture execution in a branch-only
probe. Do not change backend after the same process has already cached codecs;
use separate processes. Sources:
[environment flag](https://github.com/bytedance/sonic/blob/ba8b7aa902f6d93014423b89a4290a3955f85d6d/internal/encoder/vars/const.go),
[public exports](https://github.com/bytedance/sonic/blob/ba8b7aa902f6d93014423b89a4290a3955f85d6d/encoder/encoder_native.go).

The ARM64 assembler names generated functions `encode_<type>`, and its loader
uses a `sonic.jit.` file/module prefix. A CPU profile containing those generated
encoder frames can substantiate execution, beyond checking the mode flag.
The local runtime probe resolves the cached function as `encode_compare.Book`;
the file/module prefix is not necessarily part of the function name.
The loader requests `NoPreempt: true`; that flag alone is not proof of GC or
unwind correctness. Sources:
[encoder assembler](https://github.com/bytedance/sonic/blob/ba8b7aa902f6d93014423b89a4290a3955f85d6d/internal/encoder/arm64/assembler_regabi_arm64.go),
[JIT loader](https://github.com/bytedance/sonic/blob/ba8b7aa902f6d93014423b89a4290a3955f85d6d/internal/jit/assembler_arm64.go).

## Decoder and native kernels

The ARM64 decoder still selects `optdec.Decode` and enables the optimized
decoder/fast-map paths. It does not select `jitdec`. Therefore a decode result
for this branch must not be described as an ARM64 decoder-JIT measurement.
Its decoder API build constraint includes Go 1.25 and excludes Go 1.27+.
Source:
[ARM64 decoder selection](https://github.com/bytedance/sonic/blob/ba8b7aa902f6d93014423b89a4290a3955f85d6d/internal/decoder/api/decoder_arm64.go).

The native dispatcher defaults to `useNeon()`. It enables SVE alternatives
only when its CPU detector accepts model `0xd02` or `0xd06`, and the relevant
environment variable equals `1`: `SONIC_USE_SVE_WRAPGOC` or
`SONIC_USE_SVE_LINKNAME`. The detector checks gopsutil CPU information and
then `/proc/cpuinfo`. Leave both variables unset for the M1 experiment;
default dispatch then chooses NEON regardless of the detector result.
The existence of SVE packages in the build does not establish SVE execution.
Source:
[native dispatch and CPU detection](https://github.com/bytedance/sonic/blob/ba8b7aa902f6d93014423b89a4290a3955f85d6d/internal/native/dispatch_arm64.go).

## Loader dependency and Darwin validation boundary

The branch's root module requires loader `v0.5.2-dev.arm64.0` and replaces it
with `./loader`. Its workspace also includes that loader module. A consuming
benchmark module must explicitly replace **both Sonic and its loader** with
the matching checkout; dependency-module replacement directives are ignored.
Otherwise the preexisting stable loader requirement can select a different
implementation. Record `go list -m -json` or `go version -m` replacement data.
Sources:
[branch go.mod](https://github.com/bytedance/sonic/blob/ba8b7aa902f6d93014423b89a4290a3955f85d6d/go.mod),
[branch go.work](https://github.com/bytedance/sonic/blob/ba8b7aa902f6d93014423b89a4290a3955f85d6d/go.work),
[Go replacement rules](https://go.dev/ref/mod#go-mod-file-replace).

The ARM64 feature PR reports testing on Linux/arm64. It does not establish
Darwin compatibility. In this pinned checkout, the loader's non-Windows path
uses raw mmap/mprotect syscalls: allocate RW, copy text, change to RX. A search
of the loader and JIT source found no Darwin-specific allocation implementation
or explicit instruction-cache invalidation. This is an audit observation,
not proof of a failure; test the untouched native backend before claiming
M1 performance. Sources:
[PR #947 validation](https://github.com/bytedance/sonic/pull/947),
[mmap implementation](https://github.com/bytedance/sonic/blob/ba8b7aa902f6d93014423b89a4290a3955f85d6d/loader/mmap_unix.go),
[code installation](https://github.com/bytedance/sonic/blob/ba8b7aa902f6d93014423b89a4290a3955f85d6d/loader/moduledata.go).

## Comparison conditions

Use four named conditions where they pass the same fixture validation:

1. Current jjs checkout.
2. Sonic v1.15.5, its ARM64 VM baseline.
3. Sonic development commit above, default ARM64 JIT.
4. The same development commit and loader with `SONIC_ENCODER_USE_VM=1`.

The fourth condition isolates backend selection within one source revision;
stable-versus-development results also include other implementation and
dependency differences. Keep JSON types, string ownership, HTML escaping,
map-order policy, warmed state, operation count and toolchain matched.
Report failures before throughput: failing correctness or crashing is not a
slow benchmark sample. Any Darwin adaptation must be separately labeled with
its exact diff, rather than attributed to the untouched upstream commit.

## Local verification

The untouched development checkout and its own loader build on this M1 Pro.
The comparison module's tests pass with default JIT and forced VM, including
all 24 standalone fixture/operation/codec semantic cases in each mode.
A probe compiled in a separate temporary module under the Sonic import prefix
reads the same `vars.UseVM` flag and inspects the cached program. Default mode
reports `UseVM=false`, a `vars.Encoder`, and `encode_compare.Book`; forced VM
reports `UseVM=true` and `*ir.Program`. The probe does not modify upstream code.
These checks establish this experiment's paths, not exhaustive Darwin support.
