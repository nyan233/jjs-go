# Sonic ARM64 JIT development branch on M1 Pro

Measured 2026-11-24 on Apple M1 Pro (10 cores, 32 GiB), macOS 27.0.1,
Go 1.25.0 darwin/arm64, AC power. The development source is
`ba8b7aa902f6d93014423b89a4290a3955f85d6d`, the observed tip of
`feat/dev_hw_arm`. Its source and bundled loader were used without patches.
jjs codec source remains `2ebe820`; subsequent commits add diagnostic tooling.
The project dependency remains Sonic v1.15.5; the development replacement
exists only in an isolated benchmark copy.

On these fixtures, the development JIT delivers **1.59–1.82×** its own VM's
encoding throughput. jjs remains **1.45–2.10×** faster with owned output and
**3.33–6.26×** faster with reused output. First Pretouch costs approximately
21–24× more time in JIT mode. There is no ARM64 decoder JIT in this branch.

## Backend verification and correctness

Four conditions use the existing Small/Medium/Large JSON and concrete Go types:
jjs, Sonic v1.15.5 (ARM64 VM), development VM, and development JIT.
The two development conditions use the **same executable and source**;
only `SONIC_ENCODER_USE_VM=1` selects the VM in a new process. All other
backend/debug overrides are cleared. SVE overrides are unset, selecting NEON
on this M1 Pro. The development decoder still uses optdec in both conditions.

The comparison module tests passed independently for release, development JIT
and development VM. This includes all 24 standalone codec/size/operation
semantic cases per mode. Each measured run also validates before and after
its loop. The backend probe reports `UseVM=false`, a cached `vars.Encoder`,
and registered functions `encode_compare.Book` / `encode_compare.TwitterResponse`
for JIT; VM reports `UseVM=true` and `*ir.Program`. This establishes actual
backend selection, not merely the branch name. See
[source audit](SONIC_ARM64_JIT_SOURCE_NOTES.md) and
[probe source](testdata/sonic-backend-probe/main.go).

## Warmed operation time

Each of the 48 cases runs five separate one-second-target samples (240 total).
Variant order rotates/reverses between rounds. All captures are sequential,
with no concurrent builds/tests/profiles. `GOMAXPROCS=1`, `SONIC_NO_ASYNC_GC=1`.
Values are boxed once outside timing; both encoders escape HTML, validate
strings and leave map keys unsorted. Decoders receive the same byte slice
and Sonic copies strings. Compilation and validation are outside the loop.

Every sample observed at least 97.98% performance-core cycles;
no affinity or root/KPC configuration was requested. Instruction/cycle counts
are process-wide user+kernel totals, including runtime/GC work. Time and
allocation columns are medians; IPC is summed instructions / summed cycles.
Raw samples and min/max times are retained, with no residency or speed filtering.

[Raw process samples](sonic-arm64-jit-process.jsonl),
[full CSV including allocations/counters/ranges](sonic-arm64-jit-summary.csv),
[metadata and module replacements](sonic-arm64-jit-metadata.json).

### Encode: owned output

Times in microseconds per operation. VM/JIT is the same-development-commit
ratio; values above 1 mean the JIT condition took less time.

| Size | jjs | Sonic v1.15.5 VM | Dev VM | Dev JIT | Dev VM / JIT | Dev JIT / jjs |
|---|---:|---:|---:|---:|---:|---:|
| Small | 0.532 | 1.832 | 1.924 | 1.069 | 1.80× | 2.01× |
| Medium | 6.520 | 22.198 | 21.801 | 13.710 | 1.59× | 2.10× |
| Large | 251.273 | 618.286 | 651.930 | 364.012 | 1.79× | 1.45× |

### Encode: reused output buffer

Times in microseconds per operation. VM/JIT is the same-development-commit
ratio; values above 1 mean the JIT condition took less time.

| Size | jjs | Sonic v1.15.5 VM | Dev VM | Dev JIT | Dev VM / JIT | Dev JIT / jjs |
|---|---:|---:|---:|---:|---:|---:|
| Small | 0.345 | 2.005 | 2.088 | 1.147 | 1.82× | 3.33× |
| Medium | 2.315 | 23.220 | 23.902 | 14.504 | 1.65× | 6.26× |
| Large | 109.047 | 675.162 | 712.604 | 445.381 | 1.60× | 4.08× |

### Decode: reused target

Times in microseconds per operation. VM/JIT is the same-development-commit
ratio; values above 1 mean the JIT condition took less time.

| Size | jjs | Sonic v1.15.5 VM | Dev VM | Dev JIT | Dev VM / JIT | Dev JIT / jjs |
|---|---:|---:|---:|---:|---:|---:|
| Small | 1.056 | 1.672 | 1.703 | 1.704 | 1.00× | 1.61× |
| Medium | 20.896 | 27.168 | 28.436 | 27.061 | 1.05× | 1.30× |
| Large | 995.956 | 1,155.060 | 1,171.835 | 1,172.332 | 1.00× | 1.18× |

### Decode: fresh target

Times in microseconds per operation. VM/JIT is the same-development-commit
ratio; values above 1 mean the JIT condition took less time.

| Size | jjs | Sonic v1.15.5 VM | Dev VM | Dev JIT | Dev VM / JIT | Dev JIT / jjs |
|---|---:|---:|---:|---:|---:|---:|
| Small | 1.491 | 2.144 | 2.125 | 2.113 | 1.01× | 1.42× |
| Medium | 25.336 | 30.020 | 30.774 | 29.137 | 1.06× | 1.15× |
| Large | 1,190.914 | 1,175.073 | 1,172.466 | 1,204.941 | 0.97× | 1.01× |

The development JIT materially improves encoding relative to its own VM.
The stable/development VM difference is a version comparison and must not
be credited to JIT. Decoder differences are not decoder-JIT improvements:
both development conditions still run optdec, and small differences can also
reflect sampling variation or different warmed encoder caches.

## Large workload counters and allocation

| Operation | Condition | Instructions/op | Cycles/op | IPC | CPI | B/op | allocs/op |
|---|---|---:|---:|---:|---:|---:|---:|
| encode | jjs | 2,801,157 | 675,253 | 4.147 | 0.241 | 262,153.8 | 1.144 |
| encode | Sonic release VM | 6,414,734 | 1,799,372 | 3.578 | 0.279 | 273,195.7 | 106.416 |
| encode | Sonic dev VM | 6,477,335 | 1,883,862 | 3.435 | 0.291 | 273,157.1 | 106.411 |
| encode | Sonic dev JIT | 4,285,475 | 1,050,856 | 4.089 | 0.245 | 272,827.5 | 106.378 |
| encode-reuse | jjs | 1,674,062 | 328,403 | 5.093 | 0.196 | 0.1 | 0.001 |
| encode-reuse | Sonic release VM | 6,818,328 | 1,927,354 | 3.535 | 0.283 | 395,157.8 | 107.342 |
| encode-reuse | Sonic dev VM | 6,859,629 | 2,011,325 | 3.408 | 0.293 | 395,163.0 | 107.318 |
| encode-reuse | Sonic dev JIT | 4,784,058 | 1,210,502 | 3.934 | 0.254 | 395,135.4 | 107.336 |
| decode-reuse | jjs | 14,758,926 | 2,965,879 | 4.949 | 0.202 | 160,244.5 | 444.559 |
| decode-reuse | Sonic release VM | 13,259,388 | 3,344,690 | 3.948 | 0.253 | 808,969.4 | 4,099.234 |
| decode-reuse | Sonic dev VM | 13,359,019 | 3,375,784 | 3.963 | 0.252 | 808,969.0 | 4,099.226 |
| decode-reuse | Sonic dev JIT | 13,354,128 | 3,380,820 | 3.955 | 0.253 | 808,969.0 | 4,099.226 |
| decode-fresh | jjs | 15,822,512 | 3,436,736 | 4.642 | 0.215 | 394,925.3 | 587.678 |
| decode-fresh | Sonic release VM | 13,488,677 | 3,414,926 | 3.929 | 0.255 | 905,555.7 | 4,374.264 |
| decode-fresh | Sonic dev VM | 13,438,660 | 3,396,714 | 3.956 | 0.253 | 907,760.5 | 4,374.246 |
| decode-fresh | Sonic dev JIT | 13,527,498 | 3,434,321 | 3.939 | 0.254 | 905,554.4 | 4,374.245 |

These totals cover each full API pipeline, not generated code alone. In
particular, EncodeInto still has HTML-escape/allocation work, while owned
output takes a different allocation path. A larger IPC is not automatically
better algorithmic efficiency; compare retired instructions and cycles as well.

For Large encode-reuse, selecting JIT reduces development-branch instructions
from 6.86 million to 4.78 million per operation (30.3%) and cycles from
2.01 million to 1.21 million (39.8%); IPC rises from 3.41 to 3.93. This is a
measured reduction in execution work. However, allocation remains about
395 KB and 107 objects per operation in either mode. jjs executes 1.67 million
instructions and is effectively allocation-free for this reused-buffer case.
Backend compilation alone does not remove Sonic's remaining pipeline costs.

## First Pretouch cost

Five fresh processes per condition/fixture measure `sonic.Pretouch(type)`.
This includes encoder and decoder precompilation and relevant Go allocations,
but excludes process startup, package initialization, fixture loading and the
first Marshal after Pretouch. It is not complete cold request latency.
Medium and Large have the same TwitterResponse type, so their ten samples
are pooled below; JSON byte length does not change that type compilation.

| Type | Samples per condition | Release VM ms | Dev VM ms | Dev JIT ms | Dev VM allocated bytes | Dev JIT allocated bytes |
|---|---:|---:|---:|---:|---:|---:|
| compare.Book | 5 | 0.221 | 0.216 | 5.154 | 1,144,072 | 3,052,744 |
| compare.TwitterResponse | 10 | 0.487 | 0.420 | 8.940 | 1,723,144 | 6,751,192 |

JIT trades substantially more first-use compilation work for lower repeated
encoding cost. The raw probe records also include allocation counts and cached
function identities: [45 cold/backend samples](sonic-arm64-jit-cold.jsonl).

## CPU profile scope

Four separate three-second-target captures cover Large encode-reuse and
decode-reuse under development VM and JIT. Profile runs are excluded from
the performance tables. Raw CPU/heap profiles and complete text summaries
are retained; use observed symbols to confirm execution paths, and do not
assume sampled syscall weights equal actual kernel CPU cost. The prior
[PMU/profile study](PMU_COMPARISON_ARM64.md) documents the native-helper
profiling-attribution anomaly.

The VM profile contains `vm.Execute`; the JIT profile resolves executed
`encode_compare.TwitterResponse`, `encode_compare.User` and
`encode_compare.Entities` functions. JIT's remaining visible work includes
NEON quoting, UTF-8 validation and HTML escaping. The same encodeFinish path
still performs output validation/escaping after the generated encoder.
The syscall-attribution anomaly is present again, so sampled percentages
are not used to assign exact syscall or helper costs. Complete summaries:
[CPU profile text](sonic-arm64-jit-profiles.txt).

## Reproduction

Use separate copies of benchmark/compare for release and development. In both,
set the jjs replacement to the absolute current checkout. In the development
copy, replace **both** modules with the pinned source and its loader:

```sh
GOWORK=off go mod edit -replace=github.com/bytedance/sonic=/absolute/path/to/sonic
GOWORK=off go mod edit -replace=github.com/bytedance/sonic/loader=/absolute/path/to/sonic/loader
GOWORK=off GOTOOLCHAIN=local go build -mod=mod -o /tmp/profilecodec-dev ./cmd/profilecodec
# Build /tmp/profilecodec-release from the release copy with the same Go toolchain.
python3 compare-sonic-arm64.py --release-binary /tmp/profilecodec-release \
  --dev-binary /tmp/profilecodec-dev --output /tmp/sonic-arm64-results \
  --rounds 5 --seconds 1
```

The backend probe is built from testdata/sonic-backend-probe/main.go in a
separate temporary module named `github.com/bytedance/sonic/jjs-benchmark-probe`,
with the same dependency replacements. That module prefix permits inspecting
Sonic internal cache/flag data without changing its source. The saved
probe modules, exact executables, capture script, build/test logs and all
profiles are available locally at:

`/Users/rainstorm/.codex/visualizations/2026/11/23/01a20691-09a5-7d60-bdef-045ab41c91eb/sonic-arm64-jit-20261124`

This experiment does not establish compatibility with other Go versions,
SVE-capable servers, or all Darwin workloads. It does establish that the
untouched pinned ARM64 JIT runs these fixtures correctly on this M1 Pro.
