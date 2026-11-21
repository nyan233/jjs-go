# JSON codec comparison

This directory is an independent Go module so benchmark-only dependencies do
not leak into the codec's module. The fixtures and Binding/Generic split follow
Sonic's benchmark methodology.

## Libraries

- Go 1.22 `encoding/json`
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
| Medium | Sonic Twitter sample | 13,437 | 300+ keys, 6 layers |
| Large | Sonic `twitter.json` | 646,995 | 100 statuses, 10,000+ keys |

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
`GOMAXPROCS`. Run `jjs-go` only on an AVX2-capable CPU; package initialization
fails fast otherwise.

The latest local results, raw output, and bottleneck notes are in
[`RESULTS.md`](RESULTS.md) and
[`results-windows-amd64-custom-codecs.txt`](results-windows-amd64-custom-codecs.txt).

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
