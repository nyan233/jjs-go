# AVX2 Base64 on Windows amd64

Measured on 2026-11-24 with Windows 11, Go 1.22.0, an Intel Core i7-14700KF,
and `GOMAXPROCS=16`. The baseline is repository `main` revision `9bbfba9`;
the candidate uses the same checkout with the AVX2 Base64 kernel. No other
workload ran during timing.

Command:

```powershell
go test ./core/compiler -run '^$' -bench '^BenchmarkJITInline/Bytes' `
  -benchmem -benchtime=500ms -count=5
```

Values are medians of five samples. Raw output is retained in
[`results-windows-amd64-base64-before.txt`](results-windows-amd64-base64-before.txt)
and
[`results-windows-amd64-base64-after.txt`](results-windows-amd64-base64-after.txt).

## Implementation

The amd64 encoder now uses an AVX2 byte-slice kernel:

- The vector loop loads exactly 12 source bytes with a 64-bit load plus a
  32-bit insert, then writes exactly 16 Base64 bytes.
- `VPSHUFB` arranges three-byte groups; word masks, `VPMULHUW`, shifts and
  word blends form four six-bit fields per 128-bit lane.
- `VPCMPGTB`, saturating subtraction and a small `VPSHUFB` offset table map
  the fields to the standard Base64 alphabet.
- A scalar loop handles the final 0-11 bytes. Neither the vector nor scalar
  path reads past the source or writes past the expected output.
- Inputs larger than 49,152 bytes retain the preemptible chunk loop.

There is no cgo dependency. The public encoder path still owns allocation,
growth and slice publication.

## Reused-buffer microbenchmarks

| Case | Before ns/op | After ns/op | Change | Speedup |
| --- | ---: | ---: | ---: | ---: |
| Bytes0 | 21.11 | 21.32 | +1.0% | 0.99x |
| Bytes12 | 26.29 | 22.86 | -13.0% | 1.15x |
| Bytes48 | 43.85 | 26.19 | -40.3% | 1.67x |
| Bytes1024 | 524.2 | 121.2 | -76.9% | 4.33x |
| Bytes65536 | 31,957 | 8,181 | -74.4% | 3.91x |

All cases remain at 0 B/op and 0 allocs/op with a reused output slice.
The empty-slice result is effectively parity.

## Validation

- All byte lengths 0-160 at every 16-byte alignment, comparing against
  `encoding/base64`.
- Canary checks around the output for every tested length and alignment.
- 1 KiB, 49,151 B, 49,152 B, 49,153 B, 64 KiB and 1 MiB inputs.
- Native Windows amd64 full test suite.
- Linux amd64 cross-build of the full compiler test binary.
- Linux amd64 guard-page test with protected pages immediately before and
  after source and destination ranges.
- Existing ARM64 assembly tests and full codec tests remain green.
