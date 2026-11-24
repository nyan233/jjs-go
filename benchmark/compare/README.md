# JSON codec comparison

This directory is an independent Go module so benchmark-only dependencies do
not leak into the codec's module. The fixtures and Binding/Generic split follow
Sonic's benchmark methodology.

## Libraries

- `encoding/json` from the Go toolchain named in each results report
- `github.com/goccy/go-json` v0.11.0
- `github.com/json-iterator/go` v1.1.12
- `github.com/mailru/easyjson` v0.9.2
- `github.com/bytedance/sonic` v1.15.5
- the local `jjs-go` checkout through a `replace` directive

EasyJSON is excluded from Generic because generic values do not have a
code-generation target. Sonic uses `ConfigStd` so escaping and integer decoding
stay compatible with the other codecs.

## Fixtures

| Size | Source | JSON bytes | Shape |
| --- | --- | ---: | --- |
| Small | Sonic `Book` | 365 | 11 keys, 3 layers |
| Medium | Sonic Twitter sample | 13,023 | 300+ keys, 6 layers |
| Large | Sonic `twitter.json` | 631,514 | 100 statuses, 10,000+ keys |

The exact upstream sources and license are recorded in
[`testdata/SOURCE.md`](testdata/SOURCE.md).

## What is measured

`BenchmarkEncoder_Binding` and `BenchmarkDecoder_Binding` decode or encode
concrete Go structs. `BenchmarkEncoder_Generic` and
`BenchmarkDecoder_Generic` use `any`/`map[string]any` values produced by
`encoding/json`.

Every codec is invoked once before `b.ResetTimer()`. This warms the JIT,
Sonic `Pretouch`, jsoniter metadata, and EasyJSON initialization. Reports
include `ns/op`, `B/op`, `allocs/op`, and MB/s based on fixture bytes.

`BenchmarkDecoder_Binding` reuses a destination across iterations.
`BenchmarkDecoder_FreshBinding` uses a zero-value destination each time, so
its results include allocating the decoded slices, maps and pointers.
Generic decoding resets the destination to `nil` on every iteration.

Map key ordering is not normalized across codecs. In particular, jjs-go
encodes maps without sorting keys, while `encoding/json` and Sonic `ConfigStd`
sort them. Generic encoding results include this behavior difference.

## Run

```powershell
cd benchmark/compare
go test -run '^$' -bench '.*_Binding' -benchmem -benchtime 2s -count 3
go test -run '^$' -bench '.*_Generic' -benchmem -benchtime 2s -count 3
```

Or run the ready-made script:

```powershell
./run.ps1
./run.ps1 -Bench '.*_Binding' -BenchTime 2s -Count 5
```

Run correctness checks and regenerate EasyJSON code with:

```powershell
go generate ./...
go test ./...
```

Compare results only on the same machine, Go toolchain, power mode, and
`GOMAXPROCS`. On amd64, jjs-go requires AVX2 and SSE4.2; Apple Silicon uses
the native ARM64 backend.

The Windows amd64 results are summarized in [`RESULTS.md`](RESULTS.md). The
raw three-sample output behind that summary is
[`results-windows-amd64-final.txt`](results-windows-amd64-final.txt).

The latest native Apple M1 Pro comparison uses `main` revision `2813ca6`,
after typed JIT encoder inlining, typed map encoders and NEON Base64 merged to
`main`. It is summarized in [`RESULTS_ARM64.md`](RESULTS_ARM64.md), with all
558 samples in
[`results-darwin-arm64-main-2813ca6.txt`](results-darwin-arm64-main-2813ca6.txt).
It includes the original four comparison families, fresh-destination decode and
unsorted-map encoder controls. The host control that temporarily demoted the
CPU-heavy Telegram process is documented in the report and
[metadata](results-darwin-arm64-main-2813ca6-metadata.json). The earlier
[`cb635eb report`](RESULTS_ARM64_cb635eb.md) and
[`96ffa5b report`](RESULTS_ARM64_96ffa5b.md) with their raw output are
preserved. Reproduce the full suite on this Go 1.25.0 darwin/arm64 setup with:

```sh
cd benchmark/compare
SONIC_NO_ASYNC_GC=1 GOMAXPROCS=10 go test -run '^$' \
  -bench '^(BenchmarkEncoder_|BenchmarkDecoder_)' \
  -benchmem -benchtime=1s -count=6 -cpu=10
```

The published run built the test binary first and then invoked it with the
equivalent `-test.*` flags to keep compilation separate from measurement.

The ARM64 before/after study for the subsequent hot-path changes is in
[`HOTPATH_OPTIMIZATION.md`](HOTPATH_OPTIMIZATION.md).

The typed JIT inlining and NEON Base64 branch is measured separately in
[`JIT_INLINE_ARM64.md`](JIT_INLINE_ARM64.md), including code size and uncached
compilation costs. Its algorithm sources are in
[`BASE64_ARM64_RESEARCH.md`](BASE64_ARM64_RESEARCH.md).

The subsequent typed-map JIT and direct NEON quoting changes are measured in
[`JIT_MAP_ARM64.md`](JIT_MAP_ARM64.md), including iterator allocations,
Large-fixture rechecks and code-size costs. Runtime compatibility details
are in [`MAP_ITERATOR_RESEARCH.md`](MAP_ITERATOR_RESEARCH.md).

Static pointer checks for recursive, large and deep pointees are measured in
[`JIT_POINTER_ARM64.md`](JIT_POINTER_ARM64.md), including the remaining Go
call bridge, regression coverage and compilation costs.

## Code generation metrics

The compiler exposes `EncoderCodegenStats` and `DecoderCodegenStats` from
`core/compiler`. They report instruction counts before/after IR optimization,
folded and removed instructions, virtual-register count, physical-register
reuse, spills, generated code size, and direct-decoder plan size.

Inspect the current metrics with:

```powershell
go run ./cmd/codegenstats
```

For hardware counters such as LLC misses, IPC and branch mispredictions on
Windows, see [`PROFILING.md`](PROFILING.md) and the elevated
[`profile-hardware.ps1`](profile-hardware.ps1) harness.
