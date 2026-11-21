# Hardware-counter profiling on Windows

Go's CPU profile samples elapsed stacks. It does not expose hardware counters,
so it cannot directly report IPC, cache misses, or branch misprediction rates.
Use one of the hardware-counter tools below.

Measured results and limitations are recorded in
[`HARDWARE_RESULTS.md`](HARDWARE_RESULTS.md).

## Tool availability check

`wpr -pmcsources` on the verification machine reports these usable sources:

| Counter | Purpose |
| --- | --- |
| `LLCMisses` | Last-level cache demand misses |
| `CacheMisses` | Architecture cache-miss event |
| `BranchInstructionRetired` | Retired branches |
| `BranchMispredictsRetired` | Retired branch mispredictions |
| `InstructionRetired` | Retired instructions |
| `UnhaltedCoreCycles` | Core clock cycles |

PMU sessions currently require elevation on this machine. A normal user can
query the counter list, but WPR/xperf cannot start system performance tracing:

```text
Failed to enable the policy to profile system performance.
```

There are also existing `NT Kernel Logger` sessions owned by other software.
Do not stop them; use an elevated session when possible.

## xperf/WPR

Run from an elevated PowerShell:

```powershell
cd benchmark/compare
./profile-hardware.ps1 -Library jjs-go -Direction Encoder
./profile-hardware.ps1 -Library jjs-go -Direction Encoder -CounterSet Cache
./profile-hardware.ps1 -Library sonic -Direction Encoder
./profile-hardware.ps1 -Library jjs-go -Direction Decoder -CounterSet Interactions
./profile-hardware.ps1 -Library jjs-go -Direction Decoder -CounterSet Branches
```

For stable ratios, use the separate two-counter groups:

```powershell
./profile-hardware.ps1 -Library jjs-go -Direction Encoder -CounterSet IPC
./profile-hardware.ps1 -Library jjs-go -Direction Encoder -CounterSet LLC
./profile-hardware.ps1 -Library jjs-go -Direction Encoder -CounterSet BranchRate
./run-hardware-suite.ps1 -Focused
```

The script builds a benchmark test binary, warms JIT caches, starts
`PMC_PROFILE` with `PMCINTERRUPT` stack walking, records a 10-second Large
benchmark, and writes an ETL under `profiles/`.

If Windows already owns the single `NT Kernel Logger`, the script temporarily
updates that logger with PMU flags when invoked directly. The focused suite
stops the existing `PROC_THREAD` logger for clean per-run traces and restores
its original configuration in a `finally` block. Do not leave the system
logger stopped.

Parse an ETL with:

```powershell
./analyze-pmc.ps1 -Etl profiles/Encoder-jjs-go-IPC.etl
```

The analyzer uses the named JJS start/stop marks, filters events to
`compare.test.exe`, and writes a per-function sample CSV. Avoid dumping
unfiltered PMU events for a multi-second trace to the terminal; `xperf -a pmc`
can emit hundreds of megabytes of text.

Open the ETL in Windows Performance Analyzer. The useful derived metrics are:

```text
IPC                 = InstructionRetired / UnhaltedCoreCycles
LLC misses / Kinst  = LLCMisses / InstructionRetired * 1000
Branch misses / Kinst = BranchMispredictsRetired / InstructionRetired * 1000
Branch mispredict rate = BranchMispredictsRetired / BranchInstructionRetired
```

These are sample-derived ratios, not absolute whole-program hardware totals.
Use separate two-counter traces rather than mixing many sources. A trace
containing four counters can multiplex them and produce internally inconsistent
totals. Even two-counter IPC varied materially across repeated encoder runs on
the Intel hybrid host used for the recorded results; repeat each run and inspect
branch/LLC stability separately.

Windows ETW does not resolve the dynamically generated Go code addresses in
this project's traces; samples are grouped as `compare.test.exe!Unknown`.
Windows PMU traces are therefore suitable for process-level cache/branch
quality, while Go CPU profiles provide JIT function-level time attribution.

Run every codec separately on the same power plan and core configuration.
Hybrid Intel CPUs can expose different event support on P/E cores, so verify
the per-core counter availability in WPA rather than using only the total row.

## VTune

VTune is not currently installed. If available, the equivalent command is:

```powershell
go test -c -o profiles/compare.test.exe
vtune -collect uarch-exploration `
    -knob sampling-mode=hw `
    -knob enable-stack-collection=true `
    -result-dir profiles/vtune-jjs `
    -- profiles/compare.test.exe -test.run '^$' `
       -test.bench '^BenchmarkEncoder_Binding/Large/jjs-go$' `
       -test.benchtime 10s -test.count 1
```

Use `uarch-exploration` for CPI, memory hierarchy, branch misprediction, and
front-end/back-end bound information. Run a separate result directory for
Sonic, goccy and `jjs-go`.
