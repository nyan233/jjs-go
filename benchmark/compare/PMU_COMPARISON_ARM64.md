# M1 Pro: jjs versus Sonic, PMU and profiles

Measured 2026-11-24, Apple M1 Pro (10 cores, 32 GiB), macOS 27.0.1,
Go 1.25.0 darwin/arm64, AC power. Codec source: `2ebe820` (including the pointer
fallback fix), Sonic v1.15.5. This branch changes diagnostic tooling only.

**Status:** process IPC/CPI, CPU/heap profiles and input-copy controls are
complete. Single-case privileged branch/cache/TLB/SIMD probes succeeded and
restored the original counter configuration. Full paired worker-PMU captures
are awaiting local macOS administrator authentication. No cache/branch
comparison between the libraries is claimed yet. Xcode download was canceled
at the user's request; Xcode was not installed and SIP was not changed.

## What is being compared

Only the existing Small/Medium/Large fixtures are used. Small is `Book`;
Medium and Large are both `TwitterResponse`. Encoding uses the same pre-boxed
concrete values. Both libraries escape HTML, validate strings and use unsorted
map keys. Decoder input is `[]byte`; Sonic copies strings as ConfigStd does.
Every operation validates against encoding/json before and after measurement.

jjs emits ARM64 encoder machine code. Sonic's ARM64 encoder executes cached
type IR in a VM, with native NEON helpers. jjs's three decoder fixtures use
the direct decode-plan path; Sonic's ARM64 decoder parses natively then
converts a DOM. This measures the implemented pipelines and their helpers,
not two equivalent per-type machine-code JITs. Version-pinned source links
and platform details are in [the methodology](PMU_METHODOLOGY_ARM64.md).

| Root type / fixture | jjs generated encoder functions | Encoder bytes | Static instructions | Decoder |
|---|---:|---:|---:|---|
| Book / Small | 1 | 8,576 | 2,144 | direct plan: 11 nodes, 14 fields |
| TwitterResponse / Medium | 2 | 27,520 | 6,875 | direct plan: 22 nodes, 85 fields |
| TwitterResponse / Large | 2 | 27,520 | 6,875 | direct plan: 22 nodes, 85 fields |

These are root-type codegen statistics, excluding shared Go/ASM helpers and
additional encoders cached for dynamic interface payloads. Medium and Large
have identical static code because their Go type is identical. The static
Go-compiled Sonic `vm.Execute` body is 6,288 bytes in this binary; that is not
its complete encoder or a per-type code-size comparison. See
[raw codegen output](pmu-arm64-codegen.txt).

## Process counters and elapsed time

The standalone harness warms only the selected codec, pre-boxes the value,
then runs an exact operation count. `GOMAXPROCS=1`, `SONIC_NO_ASYNC_GC=1`.
Five rounds alternate library order; each case targets one second. Captures
run sequentially, with no concurrent builds/tests/other profiling. These
final samples were recorded after canceling Xcode's download. Preliminary
samples taken during downloading remain separate and are not in the tables.

`proc_pid_rusage(V6)` snapshots bracket the loop at stopped-child boundaries.
Instruction/cycle counts include all process threads and user+kernel work,
including GC. They are not JIT-only counters. Time, instructions/op,
cycles/op and allocation columns are medians; IPC is total instructions /
total cycles across the five samples, and CPI is its reciprocal. Allocation
counts can be fractional because of amortized arenas/pools.

All 120 primary samples are in [raw JSONL](pmu-arm64-process.jsonl),
with [CSV summary](pmu-arm64-process-summary.csv) and
[environment/binary metadata](pmu-arm64-metadata.json). Performance cores
account for at least 98.96% of cycles in every sample. This is observed
residency, not CPU pinning. A single-operation control includes all gate
and snapshot overhead; its maximum 112,858 instructions is less than
0.0011% of the smallest primary instruction total.

### Encoding with a reused output buffer

| Size | Library | µs/op | Instructions/op | Cycles/op | IPC | CPI | B/op | allocs/op |
|---|---|---:|---:|---:|---:|---:|---:|---:|
| Small | jjs | 0.329 | 6,095 | 1,006 | 6.067 | 0.165 | 0.0 | 0.000 |
| Small | sonic | 1.925 | 19,700 | 5,698 | 3.474 | 0.288 | 680.0 | 3.000 |
| Medium | jjs | 2.220 | 40,193 | 6,762 | 5.938 | 0.168 | 0.0 | 0.000 |
| Medium | sonic | 21.733 | 239,161 | 63,695 | 3.770 | 0.265 | 13,608.5 | 3.007 |
| Large | jjs | 105.783 | 1,664,228 | 323,580 | 5.144 | 0.194 | 0.0 | 0.001 |
| Large | sonic | 660.191 | 6,779,894 | 1,908,319 | 3.549 | 0.282 | 395,156.9 | 107.342 |

This removes jjs's output allocation and exposes the typed encoder/string
kernel cost. Sonic EncodeInto still allocates for HTML-escaped output and
iterator state; buffer reuse does not remove those operations.

### Encoding with an owned output buffer

| Size | Library | µs/op | Instructions/op | Cycles/op | IPC | CPI | B/op | allocs/op |
|---|---|---:|---:|---:|---:|---:|---:|---:|
| Small | jjs | 0.552 | 8,216 | 1,620 | 5.040 | 0.198 | 384.0 | 1.000 |
| Small | sonic | 1.780 | 18,806 | 5,342 | 3.530 | 0.283 | 400.1 | 2.001 |
| Medium | jjs | 6.449 | 74,912 | 17,815 | 4.218 | 0.237 | 9,472.4 | 1.006 |
| Medium | sonic | 21.109 | 229,721 | 61,768 | 3.740 | 0.267 | 9,489.1 | 2.015 |
| Large | jjs | 243.173 | 2,768,398 | 669,810 | 4.125 | 0.242 | 262,153.8 | 1.144 |
| Large | sonic | 594.568 | 6,330,078 | 1,767,692 | 3.584 | 0.279 | 273,067.9 | 106.422 |

The value is boxed once outside timing in both libraries. The original
Binding benchmarks box a concrete value at the call site, so allocation
counts here need not match the historical README table. The owned-buffer
and reused-buffer rows also include different GC/allocation work.

### Decoding into a reused target

| Size | Library | µs/op | Instructions/op | Cycles/op | IPC | CPI | B/op | allocs/op |
|---|---|---:|---:|---:|---:|---:|---:|---:|
| Small | jjs | 1.025 | 18,978 | 3,107 | 6.101 | 0.164 | 57.1 | 0.004 |
| Small | sonic | 1.604 | 22,050 | 4,808 | 4.568 | 0.219 | 544.0 | 10.000 |
| Medium | jjs | 20.197 | 308,292 | 60,628 | 5.093 | 0.196 | 3,361.5 | 1.176 |
| Medium | sonic | 26.086 | 337,760 | 78,361 | 4.296 | 0.233 | 16,936.4 | 110.005 |
| Large | jjs | 970.973 | 14,693,919 | 2,938,917 | 5.003 | 0.200 | 160,246.0 | 444.558 |
| Large | sonic | 1,098.700 | 13,131,650 | 3,267,338 | 4.018 | 0.249 | 808,969.3 | 4,099.231 |

### Decoding into a fresh target

| Size | Library | µs/op | Instructions/op | Cycles/op | IPC | CPI | B/op | allocs/op |
|---|---|---:|---:|---:|---:|---:|---:|---:|
| Small | jjs | 1.454 | 22,929 | 4,265 | 5.450 | 0.183 | 541.1 | 6.004 |
| Small | sonic | 1.982 | 26,556 | 5,963 | 4.471 | 0.224 | 1,048.0 | 21.000 |
| Medium | jjs | 23.721 | 330,843 | 70,037 | 4.770 | 0.210 | 8,753.7 | 19.179 |
| Medium | sonic | 29.037 | 356,727 | 85,899 | 4.177 | 0.239 | 21,520.5 | 145.007 |
| Large | jjs | 1,140.879 | 15,681,859 | 3,330,260 | 4.716 | 0.212 | 394,920.9 | 587.678 |
| Large | sonic | 1,170.863 | 13,484,842 | 3,406,897 | 3.979 | 0.251 | 907,975.5 | 4,374.268 |

The large decode result is the important qualification: jjs uses fewer
cycles but retires more instructions. Its higher IPC does not establish a
more efficient decoding algorithm. Fresh-target Large is close in elapsed
time despite substantially different allocation counts.

## CPU profiles and attribution limits

All 24 primary cases have separate four-second-target CPU captures. Go pprof
resolves the generated `jjs.jit.*` functions and the NEON routines. The raw
text summaries, including symbolizer warnings, are retained in
[profile summaries](pmu-arm64-profiles.txt).

For Large reused-buffer jjs encoding, `quoteJSONString` accounts for 44.0%
of the primary profile's flat samples; the independent diagnostic sees 51.2%.
Generated encoder bodies are also prominent. Remaining helper work includes
dynamic interface dispatch, float formatting and map iteration. Generated
code is executing and visible in the profile; compilation is outside timing.

Sonic's Large encode profile includes `vm.Execute`, NEON quoting, UTF-8
validation and a separate HTML-escape pass. Source shows EncodeInto's finish
step calls HTMLEscape with a nil destination. The extra passes/allocations
are part of this compatible API configuration, not only a register-allocation
or instruction-scheduling difference. Source:
[Sonic encoder finish](https://github.com/bytedance/sonic/blob/v1.15.5/internal/encoder/encoder.go).

For Large reused-target jjs decoding, `parseStringInternal` is 11.0% flat /
25.2% cumulative in the primary profile, `skipSpaceRun` is 11.9% flat, and
string-key map lookup/field dispatch also appear. The fixture has 164,608
bytes (26.07%) of whitespace outside strings; Medium has 4,109 (31.55%),
Small none. This makes whitespace scanning a concrete optimization target
for these inputs. String parsing and unknown-field skipping are additional
candidates. These profiles do not measure the benefit of a new implementation.

There is a material profiling anomaly. In a simultaneous pprof+rusage
diagnostic, Sonic Large encoding assigns 26.2% of samples to runtime.kevent,
but kernel CPU time is only 4.65% of process CPU time. For decoding the
corresponding values are 38.6% and 5.09%. jjs's decode profile also over-weights
kevent/madvise relative to measured kernel CPU time. Therefore these syscall
sample percentages are not used as actual syscall CPU costs or evidence of
an I/O bottleneck. Hardware instructions/cycles and process CPU accounting
are the stronger quantitative evidence; the exact sampling-attribution cause
has not been isolated. No samples have been removed from the saved profiles.

## Allocation profiles and the input-copy control

Allocation captures run separately with MemProfileRate=1, using a warmed
baseline heap profile and an end profile. Their elapsed times are deliberately
excluded from performance comparisons: recording every allocation is costly.
The B/op and allocs/op tables above come from ordinary counter runs. Profile
summaries hide profiler/compression allocation stacks while preserving raw
profiles; displayed percentages retain pprof's original total denominator.

Large reused-buffer jjs encoding is effectively allocation-free after warmup.
Sonic's allocation objects are dominated by IteratorStart: the capture has
104 iterator allocations per operation. Its allocation bytes are dominated
by GrowSlice underneath HTMLEscape. This explains why EncodeInto still
allocates even with an existing destination.

For jjs Large decoding, allocation bytes primarily come from copyString
arenas and dynamic interface maps/arrays/boxing. A reused struct still has
dynamic JSON payloads that must be reconstructed. Sonic's largest allocation
site is frozenConfig.Unmarshal, which converts the complete byte input to
a string; additional allocations come from DOM string conversion and generic
values. Source:
[Sonic Unmarshal](https://github.com/bytedance/sonic/blob/v1.15.5/sonic.go).

A separate three-sample API control confirms the input-copy effect:

| Size | Bytes API µs/op | Prepared String API µs/op | Bytes API B/op | String API B/op | Removed allocs/op |
|---|---:|---:|---:|---:|---:|
| Small | 1.616 | 1.440 | 544 | 160 | 1 |
| Medium | 27.074 | 22.102 | 16,936 | 3,368 | 1 |
| Large | 1,089.661 | 915.518 | 808,967 | 169,979 | 1 |

This is a diagnostic with a deliberately preconverted string, not a
replacement for the primary equal-byte-input comparison. Large's prepared
string API removes about 639,000 B/op and reduces time about 16%; jjs's allocation-byte
advantage over Sonic's byte API therefore cannot all be credited to its
parser or code generation. The remaining object-count difference is large.
[All 18 control samples](pmu-arm64-sonic-input-copy.txt) are retained.

## Worker PMU status

Without Xcode, root capability probes successfully collected:

- retired branch count and branch mispredictions;
- retired L1D load misses and demand L1I misses;
- L1D and L2 data-TLB misses;
- retired SIMD/FP ALU and integer/SIMD load counts.

The helper locks the worker to one OS thread and requests user-only
configurable events. It preserves the normal OS fixed/power counter mask,
refuses an existing configurable capture, and restores state on normal/error
exits. All four probes restored force=0, counting=5, thread_counting=0.
XNU virtual thread counters can have negative uint64 offsets after a
configuration switch. Raw before/after values are retained; modular deltas
are bounded/checked against fixed instructions. Three independent probe
groups reported about 6,035 user instructions per Small jjs operation.

The full library-by-size comparison is pending local administrator
verification; isolated capability probes are not used to claim comparative
cache/branch performance. The local M1 descriptor exposes no L2 data-cache
miss event. L2 TLB misses will not be labeled L2 cache misses. INST_SIMD_ALU
includes scalar FP and is not a pure NEON effectiveness metric.

## Reproduce and inspect

From benchmark/compare:

```sh
go build -o /tmp/profilecodec ./cmd/profilecodec
python3 profile-macos.py --binary /tmp/profilecodec --output /tmp/process-counters \
  --rounds 5 --seconds 1
python3 profile-macos.py --binary /tmp/profilecodec --output /tmp/cpu-profiles \
  --phase cpu --rounds 1 --seconds 4
python3 profile-macos.py --binary /tmp/profilecodec --output /tmp/alloc-profiles \
  --phase allocations --rounds 1 --seconds 0.1
# Optional worker PMU; repeat with cache, tlb and simd:
sudo python3 profile-macos.py --binary /tmp/profilecodec --output /tmp/worker-branch \
  --pmu branch --operation encode-reuse --operation decode-reuse --rounds 3 --seconds 1
```

The root counters require cgo and macOS. The command also builds without cgo,
and on other platforms, with an explicit unsupported error only if PMU is
requested. Ordinary process-counter and profile captures need no root. The
recorder serializes captures with a shared file lock.

Full profiles and the exact measured executable are retained locally at:

`/Users/rainstorm/.codex/visualizations/2026/11/23/01a20691-09a5-7d60-bdef-045ab41c91eb/jjs-pmu-20261124`

Use alloc_space/alloc_objects for allocation traffic and inuse_space for
retention; a post-GC heap delta is not a leak diagnosis. The available profile
snapshots cover one final retained value and pools, not long-running growth.

## Next optimization priorities

1. Reduce Large decode work: whitespace runs, string scanning and field-name
   dispatch. Confirm each change with equal semantics and fresh/reused targets.
2. For encoding, focus on the quote kernel and dynamic-interface/float work;
   the typed JIT loop already removes substantial VM dispatch work.
3. Preserve buffer reuse in callers that can use it. Allocation/GC costs in
   the owned-buffer path are material and independent of JIT instruction count.
4. Wait for the paired worker counters before diagnosing branch prediction,
   cache capacity or TLB pressure. Static code size and IPC alone do not
   establish those bottlenecks or justify adding SSA.
