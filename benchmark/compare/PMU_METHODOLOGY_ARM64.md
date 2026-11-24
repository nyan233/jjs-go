# M1 Pro PMU and profile comparison methodology

Research date: 2026-11-24. Target: this repository's Small, Medium and Large
JSON fixtures, native Apple M1 Pro, macOS 27.0.1, Go 1.25.0, Sonic v1.15.5.
This document records source checks and a measurement protocol. Event names
listed below are capabilities advertised by the local OS, not measured values.

## What the ARM64 implementations execute

Sonic's ARM64 encoder is a type-specific IR interpreter. The `!amd64`
initialization calls `ForceUseVM`; that installs `vm.EncodeTypedPointer` and
compiles a cached IR program. `vm.Execute` advances a program counter and
switches on IR opcodes. It does not produce a separate ARM64 machine-code
encoder for each Go type. The VM also calls native routines, so this does not
mean all its work is scalar Go code. Sources: Sonic v1.15.5
[backend selection](https://github.com/bytedance/sonic/blob/v1.15.5/internal/encoder/pools_compt.go),
[compiler selection](https://github.com/bytedance/sonic/blob/v1.15.5/internal/encoder/compiler.go),
[VM execution](https://github.com/bytedance/sonic/blob/v1.15.5/internal/encoder/vm/vm.go).

Sonic's ARM64 decoder selects `optdec.Decode`, builds a native parser context,
then converts the parsed document through cached `decFunc.FromDom` objects.
This is distinct from Sonic's x86 JIT decoder. Sources:
[ARM64 selection](https://github.com/bytedance/sonic/blob/v1.15.5/internal/decoder/api/decoder_arm64.go),
[decode pipeline](https://github.com/bytedance/sonic/blob/v1.15.5/internal/decoder/optdec/decoder.go),
[decoder objects](https://github.com/bytedance/sonic/blob/v1.15.5/internal/decoder/optdec/functor.go).

jjs emits type-specific ARM64 encoder machine code in
[`jit_encoder.go`](../../core/compiler/jit_encoder.go). Its decoder prefers a
cached direct decode plan when that plan supports the type; see
[`decoder.go`](../../core/compiler/decoder.go) and
[`direct_decoder.go`](../../core/compiler/direct_decoder.go). Report the actual
selected paths for the three fixture types. A fair ARM64 study compares these
executed implementations, rather than describing them as two equivalent native
JITs. Generated machine-code bytes and Sonic IR-program bytes are not the same
metric.

## Suitable Apple tools

CPU Counters can measure events and classify delivery, processing and discarded
work. Its sampling modes can help locate expensive instructions. The absence
of an apparent pipeline bottleneck does not establish an efficient algorithm.
Source: [Apple CPU bottleneck workflow](https://developer.apple.com/documentation/xcode/addressing-cpu-bottlenecks).

CPU Profiler samples using CPU cycles, whereas Time Profiler samples time.
Apple recommends CPU Profiler for CPU optimization, particularly because
asymmetric CPUs and changing frequency can bias time-based comparisons.
Use a separate Time Profiler capture when investigating scheduling or waiting.
Source: [Apple WWDC25 CPU performance session](https://developer.apple.com/videos/play/wwdc2025/308/).

Processor Trace requires a Mac with M4 or later. Installing newer Xcode does
not add that facility to M1 Pro; CPU Counters is the relevant hardware-counter
route here. Source: [Processor Trace supported hardware](https://developer.apple.com/documentation/xcode/analyzing-cpu-usage-with-processor-trace).

Xcode 26 introduced guided CPU Counter modes and retained manual event
configuration. Xcode 27 adds `xctrace record --show-recording-options` and
`--recording-options <json path>` to inspect and supply recording options.
Use the installed tool's help and actual template options instead of guessing
JSON keys or copying a template from another Xcode version. Sources:
[Xcode 26 Instruments changes](https://developer.apple.com/documentation/xcode-release-notes/xcode-26-release-notes),
[Xcode 27 Instruments changes](https://developer.apple.com/documentation/xcode-release-notes/xcode-27-release-notes).

After installation, establish the local CLI contract first:

```sh
export DEVELOPER_DIR=/Applications/Xcode.app/Contents/Developer
xcrun xctrace version
xcrun xctrace help record
xcrun xctrace help export
xcrun xctrace list templates
xcrun xctrace record --template 'CPU Counters' --show-recording-options
```

Record a warmed standalone codec loop using the installed `record` command's
launch or attach mode. Retain the exact template/options JSON. Export the
trace's table of contents before selecting counter tables; schema names and
whether an entity contains raw deltas or derived aggregates must be checked
from the actual trace. Export with `--toc`, then select an entity using
`--xpath`. The CLI supports table exports, but exporting XML does not by itself
establish a counter's semantics. Apple documents table export in the
[Xcode 13 release notes](https://developer.apple.com/documentation/xcode-release-notes/xcode-13-release-notes).

## Local M1 Pro event inventory

Read-only host queries returned `hw.cputype=16777228`, `hw.cpusubtype=2`,
`hw.cpufamily=458787763` (`0x1b588bb3`), and Performance/Efficiency performance
levels. The matching Apple-supplied descriptor is
`/usr/share/kpep/cpu_100000c_2_1b588bb3.plist`, internal name `a14`, SHA-256
`45b779879acf8eefb522d0027653e161dce44f56405f3ce4b74fc358fcdfe908`.
The following meanings come from that file, read on this host:

| Event | Meaning / interpretation |
|---|---|
| `Cycles` / `FIXED_CYCLES` | Fixed cycle counter alias; validate captured scope and units. |
| `Instructions` / `FIXED_INSTRUCTIONS` | Fixed retired-instruction counter; fallback is `INST_ALL`. |
| `INST_BRANCH` | Retired branches, including calls and returns. |
| `BRANCH_MISPRED_NONSPEC` | Architecturally executed mispredicted branches. |
| `BRANCH_COND_MISPRED_NONSPEC` | Retired conditional branches that mispredicted. |
| `BRANCH_INDIR_MISPRED_NONSPEC` | Retired mispredicted indirect branches, including calls and returns. |
| `BRANCH_CALL_INDIR_MISPRED_NONSPEC` | Retired mispredicted indirect calls. |
| `BRANCH_RET_INDIR_MISPRED_NONSPEC` | Retired mispredicted returns. |
| `L1D_CACHE_MISS_LD_NONSPEC` | Retired loads that missed L1 data cache. |
| `L1D_CACHE_MISS_LD` | Loads that missed L1 data cache; not described as retired-only. |
| `L1I_CACHE_MISS_DEMAND` | Demand instruction-fetch misses requiring an L1 instruction-cache fill. |
| `L1D_TLB_MISS_NONSPEC` | Retired loads/stores that missed the L1 data TLB. |
| `L2_TLB_MISS_DATA` | Loads/stores that missed the L2 TLB, not the L2 data cache. |
| `INST_SIMD_ALU` | Retired non-load/store Advanced SIMD and FP instructions. |
| `INST_INT_LD`, `INST_SIMD_LD` | Retired integer and SIMD/FP loads respectively. |
| `MAP_STALL`, `MAP_STALL_DISPATCH` | Map-unit stall cycles; the latter is dispatch back pressure. |

No L2 data-cache miss event is present in this matched CPU descriptor. Do not
substitute `L2_TLB_MISS_DATA`, or assume a newer chip's event numbers apply.
Check the installed Instruments modes for additional supported metrics and
mark unavailable fields explicitly. Event presence and a successful recording
are separate checks; privilege failures or empty data are not zero misses.

The descriptor also gives per-event counter masks, so not every desired event
can necessarily run together. Use separate recorded groups when allocation of
events conflicts, retain cycles/instructions in each group where possible, and
report any multiplexing or scaling. In particular, indirect-mispredict events
include returns while `INST_BRANCH_INDIR` excludes returns: those two counters
alone do not form a valid indirect-branch miss-rate denominator.

## Measurement and normalization

The following are measurement rules for this comparison, not claims about
results already collected:

1. Use the existing Small/Medium/Large inputs and matching concrete Go types.
   Separate typed encoding, reused-target decoding, fresh-target decoding and
   generic workloads. Match map sorting, HTML escaping, UTF-8 handling and
   string ownership options; validate decoded values before measurement.
2. Warm only the selected library and type before entering the measured
   interval. Use a fixed operation count, retain an observable result, and
   record the completed count. Exclude startup, compilation, file loading and
   profile serialization from the region of interest.
3. Run unprofiled throughput, PMU, CPU sampling and allocation captures
   separately. Alternate library order, repeat captures, retain all samples,
   and keep the binary, toolchain, power mode and concurrency identical.
4. Report process-wide and worker-thread scope separately when available.
   Process counters include GC, allocator and helper work. They are not
   JIT-body-only counters unless the capture is attributed to that code range.
5. Record P/E core residency from the trace. `GOMAXPROCS=1` limits Go execution
   concurrency and `LockOSThread` binds a goroutine to an OS thread; neither is
   a claim that the thread stays on a specific physical core. If core-class
   separation is unavailable, label results as mixed residency.
6. Divide events by completed operations and input bytes. Compute aggregate
   IPC as `sum(instructions)/sum(cycles)` and CPI as its reciprocal, using
   matching scope and intervals. Do not average per-sample IPC unweighted.
7. Report branch misses per operation and per thousand instructions (MPKI).
   A branch miss percentage needs a matching executed-branch denominator.
   Cache misses per operation or MPKI do not require a guessed cache-access
   denominator; only call a value a cache miss rate if both events match.
8. Judge efficiency using time/op, cycles/op and instructions/op together.
   Higher IPC alone can reflect additional easy-to-execute work. Fewer SIMD
   instructions may process more JSON bytes even when IPC falls. Likewise,
   `INST_SIMD_ALU` includes scalar FP and does not alone measure NEON utility.

## Profiles and JIT attribution

Keep Go CPU profiles and allocation profiles as independent evidence. Compare
allocation bytes and allocation objects over the same warmed interval, then
inspect retained bytes after GC separately. `alloc_space` counts cumulative
allocation; `inuse_space` describes retained memory. They answer different
questions. Use warmup baseline/delta profiles or a sufficiently isolated
harness to prevent compilation and fixture loading dominating small workloads.
Source: [Go pprof profile semantics](https://go.dev/src/runtime/pprof/pprof.go).

The jjs loader registers generated function names, PC metadata and stack maps
with the Go runtime; see [`loader.go`](../../internal/jitloader/loader.go) and
[`compiler.go`](../../core/compiler/compiler.go). This source path does not
publish a Mach-O image or dSYM for each generated function. Consequently,
native Instruments symbolication of those regions must be verified rather
than inferred from successful Go pprof symbolication. Retain anonymous/JIT
samples and their addresses, record symbol coverage, and avoid reporting a
helper-only call tree as the whole codec's cost.

Process-level hardware totals remain useful without complete symbols. A claim
about which generated branch or load causes the misses requires additional
PC-level evidence mapped to that run's generated code. Static disassembly can
identify candidate call/branch/load patterns, but cannot establish IPC, miss
rates, speculative behavior or dynamic execution counts.

## Unprivileged process counters through `proc_pid_rusage`

This host also exposes an independent counter path that does not require
Instruments: `proc_pid_rusage(pid, RUSAGE_INFO_V6, buffer)`. A read of this
research process succeeded as uid 501 and returned nonzero total and
performance-core instruction/cycle counts. `kern.monotonic.supported` is 1.
This is a capability probe, not a codec benchmark.

The installed SDK's `rusage_info_v6` is 464 bytes: a 16-byte UUID and 56
64-bit words, including reserved fields. Allocate the complete structure;
the kernel copies the entire requested flavor. The API returns 0 on success
and -1 with `errno` on failure. XNU checks same-user access, subject to the
process security policy; it does not require root for every query. Invalid
flavors return `EINVAL`, missing processes return `ESRCH`, and zombie data may
be read from cached usage. Sources: installed SDK `sys/resource.h` and
`libproc.h`, XNU
[`proc_pid_rusage` and its security policy](https://github.com/apple-oss-distributions/xnu/blob/f6217f891ac0bb64f3d375211650a4c1ff8ca1ea/bsd/kern/proc_info.c),
[`proc_get_rusage`](https://github.com/apple-oss-distributions/xnu/blob/f6217f891ac0bb64f3d375211650a4c1ff8ca1ea/bsd/kern/kern_resource.c).

| Field / delta | Meaning |
|---|---|
| `ri_instructions`, `ri_cycles` | Task-wide accumulated instruction/cycle counts. |
| `ri_pinstructions`, `ri_pcycles` | Subset accumulated on performance cores. |
| `ri_user_time`, `ri_system_time` | User/system CPU time in Mach absolute-time ticks. |
| `ri_user_ptime`, `ri_system_ptime` | Performance-core subsets of those CPU times, also Mach ticks. |

The `P` prefix is explicitly derived from `RCT_CPU_PERFORMANCE`. Instruction
and cycle totals sum tracked exception levels, including user and kernel;
they are not user-only. The task lifetime tracks include active and exited
threads. CPU-counter support is conditional in XNU, so a successful call with
zero counters is not sufficient to compute a valid IPC. Sources:
[`task_power_info_locked`](https://github.com/apple-oss-distributions/xnu/blob/f6217f891ac0bb64f3d375211650a4c1ff8ca1ea/osfmk/kern/task.c),
[`recount` aggregation](https://github.com/apple-oss-distributions/xnu/blob/f6217f891ac0bb64f3d375211650a4c1ff8ca1ea/osfmk/kern/recount.c),
[`recount` lifetime and update model](https://github.com/apple-oss-distributions/xnu/blob/f6217f891ac0bb64f3d375211650a4c1ff8ca1ea/osfmk/kern/recount.h).

Convert time deltas with `mach_timebase_info`: `nanoseconds = ticks * numer /
denom`, preferably with integer arithmetic that avoids overflow. The local
ratio is 125/3; do not hard-code it. Apple's own recount tests perform this
conversion before reporting seconds. Sources:
[time conversion tests](https://github.com/apple-oss-distributions/xnu/blob/f6217f891ac0bb64f3d375211650a4c1ff8ca1ea/tests/recount/recount_perf_tests.c),
[performance-level tests](https://github.com/apple-oss-distributions/xnu/blob/f6217f891ac0bb64f3d375211650a4c1ff8ca1ea/tests/recount/recount_tests.c).

For the fixed-operation harness, take external snapshots at two quiescent
boundaries surrounding the warmed loop. A stopped child remains a live process
with readable usage; wait for its stopped status before each snapshot and
resume only the owned child. XNU accumulates counters on context switches and
user/kernel transitions; querying a running task is not a simultaneous
hardware snapshot of every currently running CPU. Stopping at boundaries
reduces that timing ambiguity. This protocol is a recommendation derived from
the accounting implementation, not a promise of instruction-exact boundaries.

Record setup/handshake overhead using an empty-loop control. Preserve PID and
start-time identity, reject negative deltas, require positive total cycles and
instructions, and check P subsets do not exceed totals. On this P/E system,
derive E counts as total minus P. Compute P IPC only if P cycles are nonzero;
compute E IPC only if E cycles are nonzero. P-cycle share and P-CPU-time share
are distinct residency summaries. This API provides no branch or cache events.

## Optional private KPC worker-thread supplement

The private `kperf`/`kperfdata` route can add branch and cache events without
Xcode, but changes global counter configuration temporarily. It is a separate,
privileged diagnostic, not a replacement for process-wide rusage accounting.
No privileged KPC configuration was executed during this research.

Most KPC reads and writes require root or the process authorized by ktrace.
The kernel rejects concurrent CPC use with `EBUSY`, unavailable hardware with
`ENOTSUP`, and some disallowed configurations with `EPERM`. Forcing counters
already owned by another task fails with `EACCES`. Those are unavailable
results; do not change SIP, boot arguments or security settings to bypass them.
Sources: XNU
[KPC sysctl checks](https://github.com/apple-oss-distributions/xnu/blob/f6217f891ac0bb64f3d375211650a4c1ff8ca1ea/bsd/kern/kern_kpc.c),
[KPC ownership](https://github.com/apple-oss-distributions/xnu/blob/f6217f891ac0bb64f3d375211650a4c1ff8ca1ea/osfmk/kern/kpc_common.c).

`kpc_get_thread_counters` only accepts thread ID 0, meaning the calling OS
thread. It cannot read a Go worker in a child process from a Python parent.
Use calls on both edges of the worker loop, with the goroutine locked to the
same OS thread. Counters track that thread across scheduling, not a physical
core. Other GC/runtime threads are excluded. Source:
[current-thread accumulation](https://github.com/apple-oss-distributions/xnu/blob/f6217f891ac0bb64f3d375211650a4c1ff8ca1ea/osfmk/kern/kpc_thread.c).

Resolve the following symbols with `dlopen`/`dlsym` from
`/System/Library/PrivateFrameworks/kperf.framework/kperf` and
`/System/Library/PrivateFrameworks/kperfdata.framework/kperfdata`. The local SDK
`.tbd` files export these symbols. The declarations below are the private ABI
used by the upstream [simdutf counter implementation](https://github.com/simdutf/simdutf/blob/master/benchmarks/src/apple_arm_events.h),
cross-checked against Apple's [KPC tests](https://github.com/apple-oss-distributions/xnu/blob/f6217f891ac0bb64f3d375211650a4c1ff8ca1ea/tests/cpu_counters/kpc_tests.c).
They are not a stable public Apple SDK contract; fail cleanly if unavailable.

```c
typedef struct kpep_db kpep_db;
typedef struct kpep_config kpep_config;
typedef struct kpep_event kpep_event;
typedef uint64_t kpc_config_t;

uint32_t kpc_get_counting(void);
int kpc_set_counting(uint32_t classes);
uint32_t kpc_get_thread_counting(void);
int kpc_set_thread_counting(uint32_t classes);
uint32_t kpc_get_config_count(uint32_t classes);
uint32_t kpc_get_counter_count(uint32_t classes);
int kpc_get_config(uint32_t classes, kpc_config_t *values);
int kpc_set_config(uint32_t classes, kpc_config_t *values);
int kpc_force_all_ctrs_get(int *forced);
int kpc_force_all_ctrs_set(int forced);
int kpc_get_thread_counters(uint32_t tid, uint32_t count, uint64_t *values);

int kpep_db_create(const char *name, kpep_db **db);
void kpep_db_free(kpep_db *db);
int kpep_db_event(kpep_db *db, const char *name, kpep_event **event);
int kpep_config_create(kpep_db *db, kpep_config **config);
void kpep_config_free(kpep_config *config);
int kpep_config_force_counters(kpep_config *config);
int kpep_config_add_event(kpep_config *config, kpep_event **event,
                          uint32_t flags, uint32_t *conflicts);
int kpep_config_kpc_classes(kpep_config *config, uint32_t *classes);
int kpep_config_kpc_count(kpep_config *config, size_t *count);
int kpep_config_kpc_map(kpep_config *config, size_t *map, size_t bytes);
int kpep_config_kpc(kpep_config *config, kpc_config_t *values, size_t bytes);
```

An implementation should follow this order:

1. Resolve every required symbol. Open the current CPU's event database with
   `kpep_db_create(NULL, &db)`, create a config, and call the *configuration*
   function `kpep_config_force_counters`. This last call builds a userspace
   configuration; it is distinct from acquiring hardware ownership.
2. Add events by name, retrieve classes/register count/map/register values.
   `kpep_config_kpc_map` maps insertion-order events to returned counter-array
   indexes. Its size argument and `kpep_config_kpc`'s size are bytes;
   `kpc_get_thread_counters`'s count is elements. Validate all mapped indexes.
   Do not hard-code slot positions or OR numeric event codes into registers.
3. Save the force flag and counting/thread-counting masks. A fixed/power-only
   mask (bits 1 and 4; this host reports 5), with force and thread counting both
   zero, can be normal OS accounting. Reject an existing forced owner, thread
   counting, or configurable/raw/unknown running classes instead of rejecting
   every nonzero mask. Acquire ownership
   with `kpc_force_all_ctrs_set(1)`, then save the configuration that will be
   overwritten using the corresponding class/count. Re-query hardware counter
   and register counts after acquisition: accessible masks can change when
   power-manager counters are reserved. Check KPEP's register count and mapped
   indexes against those sizes. Treat failed getters, including `UINT32_MAX`
   from an unsigned mask getter, as unavailable rather than an idle mask.
   Track which mutations succeeded so partial failure still runs cleanup.
4. Install the KPEP register config with `kpc_set_config`; enable its classes
   combined with the saved fixed/power counting mask using `kpc_set_counting`,
   then enable only the measurement classes with `kpc_set_thread_counting`. The fixed and
   configurable class masks are 1 and 2. Do not request raw-PMU or power-class
   reconfiguration. Establish a valid first thread snapshot before measurement;
   the kernel allocates a thread counter buffer through an AST.
5. Snapshot immediately before/after the loop on that same locked thread.
   Convert deltas using the event index map. Check every return code, monotonic
   deltas and positive instruction/cycle counts. Measure empty-edge overhead
   separately; no per-iteration C calls are needed.
6. On all exits, stop this capture's thread counting and configurable counting
   while preserving the original fixed bit. Restore saved configuration while
   still owning the counters, then release the acquired force ownership. The
   power class has an empty hardware mask while counters are forced; restoring
   a saved mask of 5 before release therefore does not itself restore power
   counting. After release, verify and restore the previous counting mask and
   thread-counting mask as needed. Confirm the final force/counting states,
   and report cleanup failures. Free KPEP objects
   only after hardware cleanup; do not unload libraries while callbacks or
   cleanup calls still need them. Do not change sampling periods/action IDs
   unless those states are also saved and restored.

This distinction follows `kpc_get_running`, `kpc_set_running`, and
`kpc_get_configurable_pmc_mask` in [XNU KPC common code](https://github.com/apple-oss-distributions/xnu/blob/f6217f891ac0bb64f3d375211650a4c1ff8ca1ea/osfmk/kern/kpc_common.c):
while forced, all available programmable counters belong to the configurable
class and the power-class mask is empty. Releasing ownership notifies the
power manager. Do not treat mask arithmetic as proof that the prior state
has been restored; query the final state.

For scope consistency, request user-only configurable events (the upstream ABI
uses add-event flag 1). Modern ARM64 XNU may suppress kernel counting for
userspace-configured counters even if all modes are requested. Fixed counters
retain monotonic-counter compatibility. Consequently, do not silently divide
user-only misses by fixed instructions that include kernel work. Add user-only
`INST_ALL` as a configurable event when MPKI is needed, or report misses/op
without that ratio. Source: [ARM64 event validation and `set_modes`](https://github.com/apple-oss-distributions/xnu/blob/f6217f891ac0bb64f3d375211650a4c1ff8ca1ea/osfmk/arm64/kpc.c).

Useful separate groups are user `INST_ALL` + `INST_BRANCH` +
`BRANCH_MISPRED_NONSPEC`, and user `INST_ALL` +
`L1D_CACHE_MISS_LD_NONSPEC` + `L1I_CACHE_MISS_DEMAND`. Include fixed
cycles/instructions for context if KPEP permits, with their broader scope
labeled. The M1 descriptor limits `INST_ALL` to mask `0x80`; retired branch
and branch-miss events use mask `0xe0`, so configuration conflicts must be
handled explicitly. These are proposed groups; successful collection must
still be validated on the running OS.

### Virtual counter offsets and unsigned deltas

KPC thread values are virtual 64-bit accumulators. XNU updates a thread buffer
by adding the unsigned difference between two per-CPU snapshots. Enabling a
new thread-counting configuration does not zero all existing per-CPU snapshot
buffers. A stale baseline can therefore introduce an initial virtual offset,
including one near `UINT64_MAX`. This is an inference from
[`kpc_thread.c`](https://github.com/apple-oss-distributions/xnu/blob/f6217f891ac0bb64f3d375211650a4c1ff8ca1ea/osfmk/kern/kpc_thread.c),
not a guarantee that every decreasing sample is valid.

Retain the raw before/after arrays and event mapping. Use unsigned 64-bit
subtraction for an interval, but reject an implausibly large delta or a delta
inconsistent with the fixed instruction/cycle totals. If a before value is
near `UINT64_MAX`, an after value is small, and their modular difference agrees
with repeated equivalent workloads, a virtual wrap can explain the apparent
decrease. A configuration change, counter reset or incorrect index can also
produce a decrease and must not be silently accepted as wrap. The original
[counter demo](https://gist.github.com/ibireme/173517c208c7dc333ba962c1f0d67d12)
also computes event deltas using unsigned 64-bit subtraction.

Do not mask results to the physical hardware-counter width: ARM64 XNU already
combines the physical count, shadow and reload state into the returned value.
Setting a sampling period updates shadow values and reloads hardware counters;
it is not merely a way to make absolute thread readings start at zero. Avoid
period changes for this direct-count experiment unless needed for a separately
validated reason, and save/restore them if used. Source:
[`kpc_get_configurable_counters` and `kpc_set_reload_xcall`](https://github.com/apple-oss-distributions/xnu/blob/f6217f891ac0bb64f3d375211650a4c1ff8ca1ea/osfmk/arm64/kpc.c).

## Worker QoS and P/E residency

The initial privileged batch, launched through the administrator helper,
showed substantial E-core execution; earlier unprivileged captures mostly ran
on P cores. Keep those samples as a separate scheduling condition. A different
inherited task policy is a hypothesis, not an established effect of root UID
or proof of the helper's policy. XNU inherits a parent's background flags and
QoS clamp at task creation. Its effective thread policy can then clamp a
higher requested QoS or reduce it to background. Sources:
[`task_policy_create` and task policy calculation](https://github.com/apple-oss-distributions/xnu/blob/f6217f891ac0bb64f3d375211650a4c1ff8ca1ea/osfmk/kern/task_policy.c#L730),
[`thread_policy_update`](https://github.com/apple-oss-distributions/xnu/blob/f6217f891ac0bb64f3d375211650a4c1ff8ca1ea/osfmk/kern/thread_policy.c#L1645).

Use the same explicit worker QoS condition for both libraries and all three
fixture sizes. The diagnostic CLI's optional
`-worker-qos=user-initiated` mode is designed to:

1. Lock the worker to its current OS thread before warmup and measurement.
2. Read the original requested class and relative priority using
   `pthread_get_qos_class_np(pthread_self(), &original_class, &original_relpri)`.
3. Request `pthread_set_qos_class_self_np(QOS_CLASS_USER_INITIATED, 0)`, then
   read back the requested class and relative priority. Check the returned
   errno value from each API; these functions return it directly.
4. Record the original/requested values with the sample. Keep that worker
   locked until this single-run process exits, including output and cleanup.
   Do not return the modified thread to Go's general worker pool.

This process-lifetime choice avoids an invalid restoration: the getter can
return `QOS_CLASS_UNSPECIFIED`, but the public setter rejects that value with
`EINVAL`. For a reusable worker whose original class is one of the five valid
classes, restoring the saved class and relative priority is possible and must
be checked before unlocking. Do not use `pthread_setschedparam` as a reset:
it can permanently opt the thread out of QoS. API contracts and implementation:
[Apple pthread QoS header](https://github.com/apple-oss-distributions/libpthread/blob/main/include/pthread/qos.h),
[Apple pthread QoS implementation](https://github.com/apple-oss-distributions/libpthread/blob/main/src/qos.c#L138).

Requested QoS is a scheduling input, not CPU affinity. Apple explicitly
describes P/E placement as a decision based on QoS, workload and system state;
apps and daemons may be treated differently. The pthread getter reports the
requested class and does not expose QoS overrides or prove the effective task
policy. A successful request therefore does not prove P-core execution.
Sources: [Apple's P/E scheduling guidance](https://developer.apple.com/news/?id=vk3m204o),
[Apple's pthread QoS contract](https://github.com/apple-oss-distributions/libpthread/blob/main/include/pthread/qos.h).

Report observed process-wide P-cycle fraction
`delta(ri_pcycles) / delta(ri_cycles)` and P CPU-time fraction
`delta(ri_user_ptime + ri_system_ptime) / delta(ri_user_time + ri_system_time)`
for every condition. These fractions are different quantities; neither pins a
thread or identifies a particular core. KPC worker events remain scoped to the
locked worker, while rusage includes other Go/runtime threads. If a requested
QoS condition still has mixed residency, retain that fact and avoid presenting
its aggregate IPC/cache behavior as a pure P-core machine-code comparison.
Compare only runs with matched QoS settings, capture method and similar
measured residency; retain the initial batch rather than silently replacing
or pooling it with later captures.

### Application-role launch is another scheduling condition

Apple's `taskpolicy -a` implementation sets the spawn process type to
`POSIX_SPAWN_PROC_TYPE_APP_DEFAULT` and Darwin role to `PRIO_DARWIN_ROLE_UI`.
XNU maps that role to `TASK_DEFAULT_APPLICATION`; it is not the focal
`TASK_FOREGROUND_APPLICATION` role. It also updates the application's thread
group flag. This creates an application-policy condition, not a P-core binding.
Sources: [Apple taskpolicy implementation](https://github.com/apple-oss-distributions/system_cmds/blob/408bba7453608006b89772db185defbac8fe2fd0/taskpolicy/taskpolicy.c#L275),
[XNU role mapping and spawn policy](https://github.com/apple-oss-distributions/xnu/blob/f6217f891ac0bb64f3d375211650a4c1ff8ca1ea/osfmk/kern/task_policy.c#L1976).

`taskpolicy -B -p PID` calls `setpriority(PRIO_DARWIN_PROCESS, PID, 0)`.
When a helper changes another process, XNU clears that target's external
background request; when a process changes itself, it clears its internal
request. It does not clear every background source, QoS clamp or coalition
policy. Sources: [taskpolicy background option](https://github.com/apple-oss-distributions/system_cmds/blob/408bba7453608006b89772db185defbac8fe2fd0/taskpolicy/taskpolicy.c#L209),
[XNU `do_background_proc`](https://github.com/apple-oss-distributions/xnu/blob/f6217f891ac0bb64f3d375211650a4c1ff8ca1ea/bsd/kern/kern_resource.c#L1216).

The rusage `ri_cpu_time_qos_*` buckets come from effective-QoS accounting,
unlike the pthread requested-class getter. Mostly user-initiated CPU time
therefore argues against a simple background-QoS clamp during that interval;
it still does not imply P-core placement. Source:
[XNU rusage QoS accounting](https://github.com/apple-oss-distributions/xnu/blob/f6217f891ac0bb64f3d375211650a4c1ff8ca1ea/osfmk/kern/bsd_kern.c#L1315).

Apple documents that daemon/agent process type and propagated XPC context
affect resource decisions. A requesting application's performance behavior
does not automatically carry over to its services. This supports investigating
launch context, but does not establish which responsibility, coalition or
thread-group policy an administrator helper applied in these captures.
Source: [Apple daemon and agent guidance](https://developer.apple.com/documentation/apple-silicon/tuning-your-code-s-performance-for-apple-silicon).

If the application-role smoke run remains mixed, the capture is still a valid
observation of that scheduling condition. Instruction and branch counts per
operation can describe executed work; prediction misses, cache misses, cycles
and IPC depend on the core mixture and migration as well as the code. Avoid
precise cross-library rankings of those metrics when residency differs. Use
the high-P process-rusage series for the main CPU-efficiency comparison and
label mixed KPC samples as supplementary diagnostics. Do not combine their
ratios with denominators from a separate high-P run.
