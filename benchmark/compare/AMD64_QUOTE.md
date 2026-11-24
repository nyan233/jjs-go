# Fused AVX2 JSON quoting on Windows amd64

Measured on 2026-11-24 with Windows 11, Go 1.25.0, an Intel Core i7-14700KF,
and `GOMAXPROCS=16`. Baseline is `59ac80e`; the candidate replaces the
amd64 scan/copy/Go-rune path with a fused AVX2 kernel.

## Implementation

- 32- and 16-byte AVX2 blocks classify controls, quote, backslash, HTML
  delimiters and non-ASCII bytes, then copy safe blocks directly.
- An 8-byte AVX2 path avoids a function call for short strings.
- Escapes use a 1 KiB packed table and write exactly 2 or 6 output bytes.
- UTF-8 is validated with scalar range checks and copied without decoding a
  Unicode code point. Invalid bytes produce `\ufffd`; U+2028/U+2029 are escaped.
- Calls are bounded to 64 KiB plus one complete rune. Go retains allocation,
  growth and closing-quote publication.
- The amd64 JIT calls the quote kernel directly after reserving output space.
  The generic `appendQuotedGo` remains only as a test reference on amd64/arm64.

The initial AVX2 constants are loaded from read-only vectors. Broadcasting each
constant with `VPBROADCASTB` was measured at roughly 390 ns for 16-byte strings
and was removed.

## Reused-buffer quote microbenchmarks

Each value is the median of five 300 ms samples.

| Case | Before ns/op | After ns/op | Change |
| --- | ---: | ---: | ---: |
| Unicode | 729.8 | 586.8 | -19.6% |
| UnicodeThenASCII | 443.0 | 70.22 | -84.2% |
| Escapes | 2,091 | 343.0 | -83.6% |
| HTML | 7,808 | 523.8 | -93.3% |
| ASCII8 | 5.762 | 6.822 | +18.4% |
| ASCII16 | 5.923 | 6.657 | +12.4% |
| ASCII64 | 9.950 | 7.722 | -22.4% |
| ASCII256 | 405.2 | 14.86 | -96.3% |
| ASCII1024 | 435.7 | 52.90 | -87.9% |

The 8- and 16-byte constants setup costs a few tenths of a nanosecond per call.
They are retained because medium/large strings dominate the end-to-end path.
All cases remain at 0 B/op and 0 allocs/op with a reused buffer.

## Whole-document Binding encode

Each value is the median of five 500 ms samples.

| Fixture | Before ns/op | After ns/op | Change | Speedup |
| --- | ---: | ---: | ---: | ---: |
| Small | 434.8 | 408.4 | -6.1% | 1.06x |
| Medium | 5,781 | 3,383 | -41.5% | 1.71x |
| Large | 280,793 | 167,386 | -40.4% | 1.68x |

The current three-sample encoder suite is in
[`results-windows-amd64-quote-jsonv2.txt`](results-windows-amd64-quote-jsonv2.txt).
Raw before/after quote microbenchmarks and whole-document samples are retained
in:

- [`results-windows-amd64-append-quoted-before.txt`](results-windows-amd64-append-quoted-before.txt)
- [`results-windows-amd64-append-quoted-after.txt`](results-windows-amd64-append-quoted-after.txt)
- [`results-windows-amd64-quote-before.txt`](results-windows-amd64-quote-before.txt)
- [`results-windows-amd64-quote-after.txt`](results-windows-amd64-quote-after.txt)

## Validation

- Differential tests against the old Go reference for random byte strings,
  every adjacent byte pair, all valid Unicode scalar values and malformed UTF-8.
- Exact source/destination alignment and capacity combinations, including
  checks that no byte is written outside reported progress.
- 64 KiB quote chunk boundaries and output-buffer reuse.
- Linux amd64 guard-page test for source and destination bounds.
- Native Windows amd64, macOS ARM64, Linux amd64 and ARM64 assembly/CI suites.
