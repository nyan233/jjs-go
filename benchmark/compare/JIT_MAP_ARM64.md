# Typed map JIT and direct ARM64 string quoting

Measured 2026-11-23 on Apple M1 Pro (10 cores, 32 GiB), macOS 27.0.1,
Go 1.25.0 darwin/arm64, AC power, `GOMAXPROCS=10`, `SONIC_NO_ASYNC_GC=1`.
Baseline: `3896491`; implementation: `419bd25` on `codex/jit-inline-maps`.
These are incremental changes after the primitive/slice/Base64 work in
[JIT_INLINE_ARM64.md](JIT_INLINE_ARM64.md). They have not been merged to main.

## Implementation and boundaries

The generic `jitEncodeMap` helper is removed. Generated code performs the
nil check, braces, entry loop, separators and key/value encoding. Integer key
width/sign and string handling are selected during compilation. Eligible
values are emitted into the loop; recursive or oversized values use a
separate generated unit. Interface values retain dynamic dispatch.

Map advancement still calls `reflect.MapIter.Next`. We reuse real, typed
`MapIter` objects in pooled context nodes and derive/check the offsets of
their current key/element pointers. This removes the old Go 1.25 compatibility
shim and its two allocations per map, while the Go runtime handles buckets,
Swiss groups, growth and iteration order. The dependency on the iterator's
private pointer prefix is isolated and checked; an unknown layout produces
an explicit compilation error. Research and version-specific sources are in
[MAP_ITERATOR_RESEARCH.md](MAP_ITERATOR_RESEARCH.md).

Each active nesting level has stable iterator storage. Reset clears input
references on success, error and panic. This storage is retained with the
pooled context and costs memory proportional to maximum active map depth.
Warm zero-allocation results exclude initial iterator creation, pool misses,
output allocation/growth and allocations inside custom marshalers.

ARM64 `appendQuoted` already used the handwritten NEON `quoteJSONString`
kernel before this change. Static string fields and map string keys now
perform reservation/quote handling in generated code and call that kernel
directly, bypassing the Go appendQuoted wrapper. Capacity exhaustion or the
64 KiB work budget uses the shared Go tail. Dynamic built-in strings in
interfaces still use the wrapper, which also calls the same NEON kernel.
The amd64 string path is unchanged.

Regressions also exposed pointer-to-map wrapping and named-map MarshalJSON
receiver/nil handling issues, now fixed. Go 1.26 moved the direct-interface
flag from Kind to TFlag; version-specific readers fix the previously failing
direct-interface aggregate tests. These corrections are included in the run.

## Measurement method

Prebuilt binaries ran sequentially, with no concurrent builds, tests or
profiles. Each steady-state median uses five 300 ms samples. The baseline
compiler test binary includes the same new benchmark harness with production
code at `3896491`. Microbenchmarks compile/warm the encoder before timing and
reuse output capacity. Comparison fixtures use their existing Marshal APIs.
This is a desktop measurement, not an isolated machine.

Cold compilation uses five samples of 50 fresh compilations: JIT generation
and loading repeat, but reflection/type metadata and the process can be warm.
It does not measure startup. All 470 primary samples, including unchanged
codec controls, are retained in [before](jit-map-arm64-before.txt) and
[after](jit-map-arm64-after.txt). The [metadata](jit-map-arm64-metadata.json)
records revisions, environment and benchmark selectors. No samples were
removed. Small timing differences should be treated as noise.

## Reused-buffer maps

| Case | Before ns/op | After ns/op | Time change | Before allocs/op | After allocs/op | Before code B | After code B |
|---|---:|---:|---:|---:|---:|---:|---:|
| Nil | 3.106 | 3.373 | +8.6% | 0 | 0 | 496 | 1,264 |
| Empty | 73.35 | 35.77 | -51.2% | 2 | 0 | 496 | 1,264 |
| Small | 162.4 | 104.1 | -35.9% | 2 | 0 | 496 | 1,264 |
| StringInt | 4,039 | 3,511 | -13.1% | 2 | 0 | 496 | 1,264 |
| StringString | 4,735 | 3,946 | -16.7% | 2 | 0 | 224 | 1,200 |
| IntUint | 4,300 | 2,682 | -37.6% | 2 | 0 | 480 | 1,392 |
| Struct | 5,788 | 4,810 | -16.9% | 2 | 0 | 1,056 | 2,016 |
| Nested | 6,112 | 4,013 | -34.3% | 66 | 0 | 608 | 2,128 |
| Interface | 7,300 | 6,462 | -11.5% | 2 | 0 | 224 | 992 |
| EscapedKeys | 384 | 333.6 | -13.1% | 2 | 0 | 224 | 1,200 |

StringInt, StringString, IntUint, Struct and Interface have 128 entries.
Nested contains 32 inner maps of three entries each; Small has three entries.
EscapedKeys has one heavily escaped key/value pair. All warmed cases use
zero bytes/allocations per operation after the change. The nil-map path
returns before entering generated map code; its sub-nanosecond change is
not evidence about loop performance.

## Existing comparison fixtures

| Family / size / library | Before ns/op | After ns/op | Time change | Before allocs/op | After allocs/op |
|---|---:|---:|---:|---:|---:|
| Small/goccy | 567.6 | 576.5 | +1.6% | 2 | 2 |
| Small/sonic | 1,586 | 1,591 | +0.3% | 3 | 3 |
| Small/jjs-go | 519.9 | 488.4 | -6.1% | 2 | 2 |
| Medium/goccy | 4,440 | 4,199 | -5.4% | 2 | 2 |
| Medium/sonic | 16,550 | 16,995 | +2.7% | 3 | 3 |
| Medium/jjs-go | 3,764 | 3,474 | -7.7% | 2 | 2 |
| Large/goccy | 212,460 | 217,755 | +2.5% | 2 | 2 |
| Large/sonic | 484,805 | 489,120 | +0.9% | 107 | 107 |
| Large/jjs-go | 163,370 | 142,093 | -13.0% | 210 | 2 |
| Small/goccy | 1,693 | 1,692 | -0.1% | 1 | 1 |
| Small/sonic | 4,252 | 4,313 | +1.4% | 7 | 7 |
| Small/jjs-go | 1,891 | 1,422 | -24.8% | 11 | 1 |
| Medium/goccy | 25,615 | 25,625 | +0.0% | 1 | 1 |
| Medium/sonic | 51,569 | 50,605 | -1.9% | 40 | 40 |
| Medium/jjs-go | 19,475 | 15,030 | -22.8% | 77 | 1 |
| Large/goccy | 1,321,344 | 1,297,135 | -1.8% | 1 | 1 |
| Large/sonic | 2,427,449 | 2,286,407 | -5.8% | 1,268 | 1,268 |
| Large/jjs-go | 939,231 | 785,779 | -16.3% | 2,529 | 1 |
| Small/goccy | 1,468 | 1,485 | +1.2% | 1 | 1 |
| Small/sonic | 4,186 | 4,082 | -2.5% | 7 | 7 |
| Small/jjs-go | 1,871 | 1,419 | -24.2% | 11 | 1 |
| Medium/goccy | 15,942 | 15,496 | -2.8% | 1 | 1 |
| Medium/sonic | 42,754 | 42,706 | -0.1% | 40 | 40 |
| Medium/jjs-go | 20,225 | 14,919 | -26.2% | 77 | 1 |
| Large/goccy | 874,519 | 861,332 | -1.5% | 1 | 1 |
| Large/sonic | 1,906,687 | 1,903,634 | -0.2% | 1,268 | 1,268 |
| Large/jjs-go | 926,253 | 794,628 | -14.2% | 2,529 | 1 |

Generic/Large previously allocated two iterator objects for each of 1,264
maps plus one output buffer. It now allocates only the output buffer in the
warm steady state. Binding/Large similarly removes 208 map allocations.
The elapsed-time changes combine map generation, iterator reuse and direct
string-kernel calls; this run does not isolate their individual contributions.

Ordinary Generic does not normalize map-key sorting across libraries.
UnorderedGeneric is the matching unsorted comparison: after the change,
Large jjs-go is 794.6 µs, goccy 861.3 µs and Sonic 1,903.6 µs on this setup.
The unchanged controls show variation up to about 6% between primary runs;
these are machine/toolchain-specific measurements.

## Alternating Large recheck

Five rounds alternate which binary runs first, with 500 ms per case. All
20 samples are retained in [jit-map-arm64-recheck.txt](jit-map-arm64-recheck.txt).

| Case | Before ns/op | After ns/op | Time change |
|---|---:|---:|---:|
| Generic/Large/jjs-go | 934,309 | 775,497 | -17.0% |
| UnorderedGeneric/Large/jjs-go | 928,062 | 773,380 | -16.7% |

New Generic samples range from 773,084 to 784,712 ns/op; new UnorderedGeneric
samples range from 771,196 to 790,139 ns/op. The repeated run supports the
Large improvement despite measurement-order and desktop variability.

## Code size and uncached compilation

The microbenchmark table includes total generated code size. For example,
map[string]string grows from 224 to 1,200 bytes; nested maps grow from 608 to
2,128 bytes. The TwitterResponse fixture grows from 21,152 to 27,520 bytes
(+30.1%), with two generated functions in both versions. Shared map traversal
primitives and the quote kernel are not duplicated into each field.

| Type case | Before compile ns/op | After compile ns/op | Time change |
|---|---:|---:|---:|
| Nil | 15,132 | 14,112 | -6.7% |
| Empty | 14,242 | 14,641 | +2.8% |
| Small | 14,839 | 14,210 | -4.2% |
| StringInt | 14,518 | 14,818 | +2.1% |
| StringString | 14,798 | 15,145 | +2.3% |
| IntUint | 17,521 | 15,890 | -9.3% |
| Struct | 22,943 | 21,007 | -8.4% |
| Nested | 22,403 | 21,946 | -2.0% |
| Interface | 14,628 | 13,143 | -10.2% |
| EscapedKeys | 15,005 | 15,043 | +0.3% |

These short compilation measurements show no large cold-cost increase for
the tested map types. Fewer separately loaded value functions offset some
of the larger generated loop. Nil, Empty, Small and StringInt compile the
same effective map layout and illustrate the noise in short cold runs.
The existing inline depth/type-cost limits remain; SSA is not introduced.

## Validation

- Native Go 1.25 and Go 1.22.12 full suites/builds; Go 1.26 full suite;
  Go 1.25 noswissmap; comparison-module tests and ARM64 assembler tests.
- Map differential tests cover key widths/extremes, named types, nil/empty
  maps, pointers, interfaces, recursive/nested maps, large indirect values,
  custom methods, errors and callback-triggered growth/deletion.
- Forced GC, stack growth and argument spills cover generated map/quote
  calls. Iterator cleanup is inspected after success, error and panic.
- A synthetic bool return with dirty upper register bits checks amd64's
  one-byte return ABI. Poisoned generic helpers verify typed fast paths.
- ARM64 quote boundaries cover empty/Unicode/invalid UTF-8/escaped strings,
  short vector boundaries, the 64 KiB budget and output capacity changes.
- A 20-second map fuzz run completed 69,063 executions without a failure.
- [Implementation CI](https://github.com/nyan233/jjs-go/actions/runs/45159946038)
  passed all 15 jobs: Windows amd64 Go 1.22/1.25, Linux amd64,
  macOS ARM64, Go 1.21–1.27, ARM64 cross-build and QEMU execution.
