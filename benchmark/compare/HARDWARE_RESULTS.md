# Windows hardware profiling results

Measured on 2026-11-21 with Windows Performance Recorder/Windows Performance
Analyzer 10.0.26100.

| Item | Value |
| --- | --- |
| CPU | Intel Core i7-14700KF |
| OS | Windows 11 Pro 10.0.26200 |
| Go | go1.22.0 windows/amd64 |
| Benchmark | Large typed binding, 5 seconds per trace |
| Sampling | `PMC_PROFILE` with `PMCINTERRUPT` stack collection |

VTune was not available locally and Intel's download flow required a browser
reCAPTCHA. The measurements below therefore use Windows PMU sampling through
`xperf`. Unlike VTune, this does not expose L1/L2 cache-line, TLB, frontend, or
microarchitecture bottleneck breakdowns.

## Method

Every result uses a dedicated two-counter group:

| Metric | Counters |
| --- | --- |
| IPC | `InstructionRetired`, `UnhaltedCoreCycles` |
| LLC misses per 1,000 instructions | `LLCMisses`, `InstructionRetired` |
| Branch misprediction rate | `BranchMispredictsRetired`, `BranchInstructionRetired` |

The first suite used four counters per trace. `xperf` multiplexed those
counters, and one trace reported more retired branches than retired
instructions. Those results are discarded. The focused suite uses only two
counters, records only the benchmark body between named marks, filters PMU
events to `compare.test.exe`, and flushes/closes each trace before analysis.

Windows owns a single `NT Kernel Logger`. The suite stops the existing
`PROC_THREAD` logger while recording, then restores its original 4 KB buffer,
circular mode, 100 MB limit, and 3-second flush interval. The restored logger
was queried successfully after the final run.

The counters in the tables are sample-derived totals emitted by
`xperf -a pmc`, not absolute whole-program hardware totals. The ratios are
intended for comparisons between the same fixture and sampling interval.
PMU sampling also noticeably slows the benchmark and changes its adaptive
iteration count.

## Repeated results

Two independent focused suites were recorded before the decoder allocation
changes. Branch and LLC ratios were repeatable enough to compare. IPC was not
stable across runs, even though each trace used only two counters, so the IPC
column is reported but is not used as a library-ranking result.

| Workload | Run | IPC | LLC misses / Kinst | Branch mispredict rate |
| --- | ---: | ---: | ---: | ---: |
| jjs-go encoder | 1 | 3.9677 | 0.6073 | 0.5524% |
| jjs-go encoder | 2 | 2.0128 | 0.6158 | 0.5500% |
| Sonic encoder | 1 | 2.6054 | 0.7990 | 1.1450% |
| Sonic encoder | 2 | 2.3830 | 0.8080 | 1.1512% |
| goccy encoder | 1 | 2.1382 | 0.7815 | 0.6596% |
| goccy encoder | 2 | 2.4522 | 0.6254 | 0.6583% |
| jjs-go decoder | 1 | 3.3958 | 0.4462 | 0.3734% |

Raw ratio inputs:

| Trace | Run | Instructions | Cycles | LLC misses | Branch misses | Branches |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| jjs-go encoder IPC | 1 | 1,304,564 | 328,794 | | | |
| jjs-go encoder IPC | 2 | 1,229,805 | 610,997 | | | |
| jjs-go encoder LLC | 1 | 1,348,665 | | 819 | | |
| jjs-go encoder LLC | 2 | 1,329,925 | | 819 | | |
| jjs-go encoder branch | 1 | | | | 1,740 | 314,992 |
| jjs-go encoder branch | 2 | | | | 3,648 | 663,336 |
| Sonic encoder IPC | 1 | 2,213,722 | 849,659 | | | |
| Sonic encoder IPC | 2 | 2,244,158 | 941,717 | | | |
| Sonic encoder LLC | 1 | 2,262,732 | | 1,808 | | |
| Sonic encoder LLC | 2 | 2,272,159 | | 1,836 | | |
| Sonic encoder branch | 1 | | | | 4,762 | 415,899 |
| Sonic encoder branch | 2 | | | | 4,581 | 397,928 |
| goccy encoder IPC | 1 | 2,510,607 | 1,174,195 | | | |
| goccy encoder IPC | 2 | 2,521,838 | 1,028,394 | | | |
| goccy encoder LLC | 1 | 2,565,516 | | 2,005 | | |
| goccy encoder LLC | 2 | 2,652,797 | | 1,659 | | |
| goccy encoder branch | 1 | | | | 3,950 | 598,824 |
| goccy encoder branch | 2 | | | | 3,760 | 571,189 |
| jjs-go decoder IPC | 1 | 1,604,865 | 472,606 | | | |
| jjs-go decoder LLC | 1 | 1,622,593 | | 724 | | |
| jjs-go decoder branch | 1 | | | | 2,014 | 539,360 |

The decoder was then changed to reuse existing slice capacity, clear retired
slice tail elements, reuse existing maps, and decode short escaped strings
through a stack buffer. A second focused run produced:

| Trace | Instructions | Cycles | LLC misses | Branch misses | Branches | Derived result |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| jjs-go decoder IPC | 1,469,169 | 315,403 | | | | IPC 4.6581 |
| jjs-go decoder LLC | 1,497,878 | | 409 | | | 0.2732 / Kinst |
| jjs-go decoder branch | | | | 1,443 | 485,003 | 0.2975% |

The LLC result uses only hundreds of sampled miss events per trace. Treat
sub-0.1 differences between libraries as directional rather than statistically
precise.

## Normal benchmark A/B

The normal benchmark was run with `-benchtime 2s -count 3`, without PMU
sampling. Values below are medians.

| jjs-go Large binding | Before | After |
| --- | ---: | ---: |
| Encoder | 461,121 ns/op, 272,487 B/op, 107 allocs/op | unchanged |
| Decoder | 1,185,167 ns/op, 539,627 B/op, 5,139 allocs/op | 1,097,255 ns/op, 215,477 B/op, 5,142 allocs/op |

Decoder latency improved by about 7.4%, allocated bytes fell by about 60.1%,
and the allocation count was effectively unchanged. The remaining allocation
count is dominated by decoded strings, maps/slices held in `interface{}`
fields, and dynamic interface decoding rather than the outer slice backing
array.

Changing the encoder SIMD threshold from 256 bytes to 32 bytes was also tested.
It regressed Large encoding from roughly 460 microseconds to 985 microseconds,
so the original threshold was retained.

## Go CPU profile

Hardware PMU events do not resolve generated Go code addresses in this ETL:
leaf samples appear as `compare.test.exe!Unknown`. Go's CPU profiler does
resolve the JIT symbols, so it was used only for time attribution, not for
cache or branch metrics.

Before the decoder changes, the encoder profile showed:

| Function/component | Flat | Cumulative |
| --- | ---: | ---: |
| `appendQuoted` | 22.17% | 40.48% |
| `avx2FindJSONEscape` | 10.70% | 10.70% |
| `jitEncodeInterface` | 1.99% | 21.99% |
| GC-related samples | | about 17.8% |

The interface cumulative value includes nested string encoding, so it is not a
pure dispatch cost. `sync.Map.Load` inside the dynamic interface cache remained
a measurable cost at roughly 7% cumulative.

The decoder profile showed:

| Function/component | Flat | Cumulative |
| --- | ---: | ---: |
| `parseStringInternal` | 18.54% | 39.33% |
| `skipSpace` | 14.21% | 14.21% |
| `parseStringSlow` | 3.22% | 7.70% |
| GC-related samples | | about 12.7% |

Allocation profiles attributed 43.22% of allocated bytes to
`decodeDirectSlice`, 27.52% to `parseStringSlow`, and 17.98% to `rawString`.
After the changes, `decodeDirectSlice` fell to 0.15% of allocated bytes, while
`rawString` (42.82%) and escaped-string/map decoding became the remaining
allocation sources.

## Interpretation

jjs-go is not losing to Sonic/goccy because of an obvious LLC or branch
misprediction cliff on this workload. Its encoder has lower branch
misprediction density than both comparison libraries and LLC miss density below
Sonic, but the process-level IPC ratios are too noisy on this host to support a
ranking claim. Go CPU profiles and normal benchmarks show that the dominant
cost is the absolute amount and shape of work: per-string scanning,
helper/interface dispatch, allocation, and GC.

The decoder's hardware profile improved after collection reuse: IPC rose, while
LLC miss density and branch misprediction rate fell. Continued optimization
should target string scanning, whitespace checks, dynamic interface cache
lookup, and allocations still produced by strings and dynamically typed
containers.

## Reproduction

Run from an elevated PowerShell:

```powershell
cd benchmark/compare
./run-hardware-suite.ps1 -Seconds 5 -Focused
./analyze-pmc.ps1 -Etl profiles/Encoder-jjs-go-IPC.etl
./analyze-pmc.ps1 -Etl profiles/Encoder-jjs-go-LLC.etl
./analyze-pmc.ps1 -Etl profiles/Encoder-jjs-go-BranchRate.etl
```

The ETL files and generated CSVs are written under the gitignored `profiles/`
directory.
