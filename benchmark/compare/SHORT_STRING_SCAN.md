# Short-string encoder scan optimization

Measured on 2026-11-21 with:

| Item | Value |
| --- | --- |
| CPU | Intel Core i7-14700KF |
| OS | Windows 11 Pro 10.0.26200 |
| Go | go1.22.0 windows/amd64 |
| Benchmark | Typed binding Small/Medium/Large, `GOMAXPROCS=1` |
| Command | `go test -benchtime 3s -count 3 -cpu 1` |

## Change

Previously, strings shorter than 256 bytes bypassed AVX2 and were scanned one
byte at a time. The new short-string encoder scanner combines:

- `PCMPESTRI` range matching for each 16-byte block.
- `VMOVQ` with an explicit 8-byte length for the 8-byte tail.
- `VMOVD` with an explicit 4-byte length for the 4-byte tail.
- The existing scalar checks for the remaining 1-3 bytes.
- The existing AVX2 scanner for strings of at least 256 bytes.

The SSE4.2 range table detects control bytes, quote, amp, angle brackets,
backslash, and non-ASCII bytes in one operation. Non-ASCII matches continue
through the existing UTF-8 validation and encoding path.

## Encoder A/B results

The baseline is the previous implementation with the short scanner disabled
below 256 bytes. Values are the median of three runs.

| Fixture | Before | After | Improvement |
| --- | ---: | ---: | ---: |
| Small | 1,076 ns/op | 1,063 ns/op | 1.21% |
| Medium | 11,129 ns/op | 7,205 ns/op | 35.26% |
| Large | 527,750 ns/op | 437,942 ns/op | 17.02% |

Allocations were unchanged: Small uses 2,040 B/op and 3 allocs/op, Medium
9,640 B/op and 3 allocs/op, and Large 272,311 B/op and 107 allocs/op.

## Scanner microbenchmark

Safe ASCII strings with no bytes requiring escaping:

| Length | Time |
| ---: | ---: |
| 8 B | 1.95 ns |
| 16 B | 1.98 ns |
| 32 B | 3.10 ns |
| 64 B | 5.41 ns |
| 128 B | 10.00 ns |
| 255 B | 22.92 ns |

## Decoder experiment

The same short-string scan was tested for decoder keys and values. It did not
improve the Large decoder benchmark: the short-key workload is dominated by
scalarly finding the closing quote, and vector setup did not amortize. That
branch was removed, so decoder behavior remains unchanged at approximately:

```text
Large decoder: 1.10 ms/op, 215.5 KB/op, 5,142 allocs/op
```

## Verification

Tests cover every string length from 0 through 255 bytes, every special-byte
position, the 16/8/4/scalar boundaries, non-ASCII input, and output equality
with `encoding/json`.
