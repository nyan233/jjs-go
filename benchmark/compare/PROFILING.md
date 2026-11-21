# Hardware-counter profiling on Windows

Go's CPU profile samples elapsed stacks. It does not expose hardware counters,
so it cannot directly report IPC, cache misses, or branch misprediction rates.
Use one of the hardware-counter tools below.

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

The script builds a benchmark test binary, warms JIT caches, starts
`PmcProfile`, records a 10-second Large benchmark, and writes an ETL under
`profiles/`.

Open the ETL in Windows Performance Analyzer. The useful derived metrics are:

```text
IPC                 = InstructionRetired / UnhaltedCoreCycles
LLC misses / Kinst  = LLCMisses / InstructionRetired * 1000
Branch misses / Kinst = BranchMispredictsRetired / InstructionRetired * 1000
Branch mispredict rate = BranchMispredictsRetired / BranchInstructionRetired
```

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
