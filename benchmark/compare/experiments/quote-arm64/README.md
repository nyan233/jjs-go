# ARM64 scan/copy experiment

This directory contains an independently written, bounded ASCII scan/copy
experiment and Apple Clang 21 `-O3 -mcpu=apple-m1` assembly output. The C code
is not linked into jjs-go and introduces no cgo or Clang runtime dependency.

```sh
clang -O3 -mcpu=apple-m1 scan_copy.c -o /tmp/quote-scan
/tmp/quote-scan
clang -O3 -mcpu=apple-m1 -S -Rpass=loop-vectorize \
  -Rpass-missed=loop-vectorize -Rpass-analysis=loop-vectorize scan_copy.c -o /tmp/quote-scan.s
```

Three implementations copy the same safe ASCII prefix and stop at a character
requiring scalar handling. The benchmark uses entirely safe ASCII. `scalar`
uses ordinary C conditions; `compare` explicitly compares NEON lanes against
special-byte values; `table` intersects two NEON table lookups indexed by the
low and high nibble. All input loads and output stores are bounded by `n`.

The scalar loop was not automatically vectorized: Clang reports an unsupported
switch and an unsupported early-exit control-flow shape.
Clang does emit the explicit NEON intrinsics in the other versions: its loop
remarks are not saying that these functions contain no SIMD. The table version
uses `TBL`, `USHR`, `AND`, `UMAXV` and 16-byte load/store operations. The Go
assembly implementation uses widening `UADDLV` instead of `UMAXV`; class sums
fit in 16 bits even when every lane contains a non-ASCII byte.

The C kernel does not perform escaping, UTF-8 validation or buffer growth, so
its absolute timings cannot be compared directly to the full Go API. The
production implementation is in `core/compiler/json_quote_arm64.s`, with an
8-byte tail path, packed escape entries and scalar UTF-8 validation. It never
calls the C functions. Results are in [the full study](../../APPEND_QUOTED_ARM64.md).

References consulted:

- [Sonic native quote loop](https://github.com/bytedance/sonic/blob/main/native/quote.c):
  bounded progress and copying while finding escape characters.
- [RapidJSON writer](https://github.com/Tencent/rapidjson/blob/master/include/rapidjson/writer.h):
  SIMD scan/copy of unescaped runs.
- [Arm NEON intrinsics reference](https://arm-software.github.io/acle/neon_intrinsics/advsimd.html):
  table lookup, widening reduction and byte-vector operations.
- [simdutf](https://simdutf.github.io/simdutf/index.html): SIMD Unicode validation
  is possible; this change uses scalar UTF-8 range checks in assembly instead
  of integrating a separate SIMD Unicode-validation pass.

## UTF-8 follow-up

`utf8_lookup4.c` transcribes the paper-guided validator into NEON intrinsics.
It accepts one bounded 16-byte block, rejects JSON escapes or UTF-8 errors,
and copies 13–16 bytes ending at a complete rune. It is an assembly-inspection
experiment, not a full encoder or a separately benchmarked competitor.

```sh
clang -O3 -mcpu=apple-m1 -S utf8_lookup4.c -o clang-utf8-o3.s.txt
```

The retained Clang output uses three UTF-8 `TBL` operations, `EXT` for prior
bytes, vector comparisons and an XOR of required continuation positions.
It uses immediate `MOVI` constants and conditional selects for prefix length.
The Go kernel loads its UTF-8 tables once per call on demand and retains
branches for prefix selection. A version using Clang-style immediate constants
showed a regression on invalid bytes and was not retained. Its scalar
fallback must preserve byte-wise replacement and bounded-output progress.
See [the paper study](../../QUOTING_PAPERS_ARM64.md) for full-codec results;
the C function is never linked into Go.
