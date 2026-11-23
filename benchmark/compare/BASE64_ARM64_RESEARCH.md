# ARM64 Base64 encoding for the JIT

Research date: 2026-11-23. Target: Apple M1 Pro, Go 1.25.0, baseline
`ca8e881`. This is a source-based design note, not a performance result.

## Recommended first implementation

Use a bounded NEON kernel with **48 input bytes → 64 output bytes** per
iteration. `LD3` supplies three deinterleaved vectors; shifts extract four
vectors of six-bit indices; four `TBL` instructions map them through the
64-byte Base64 alphabet; `ST4` interleaves the output. This is already the
ARM64 strategy in [aklomp/base64 v0.5.2](https://github.com/aklomp/base64/blob/v0.5.2/lib/arch/neon64/enc_loop.c)
and [simdutf v7.7.0](https://github.com/simdutf/simdutf/blob/v7.7.0/src/arm64/arm_base64.cpp#L147-L194).
It directly fits the fixed standard alphabet needed by JSON byte slices.

For each input triple `a, b, c`, the lane-wise index computation is:

```text
u0 = a >> 2
u1 = ((a << 4) | (b >> 4)) & 63
u2 = ((b << 2) | (c >> 6)) & 63
u3 = c & 63
```

Implement the two shifted ORs with `SLI`, after `USHR` initializes the
destination. The operation count is three `USHR`, two `SLI`, and three
`AND`, followed by four table lookups. See the separate
[bit reshuffling implementation](https://github.com/aklomp/base64/blob/v0.5.2/lib/arch/neon64/enc_reshuffle.c).
These are derived index formulas, not a verbatim source translation.

Arm's [NEON intrinsic reference](https://arm-software.github.io/acle/neon_intrinsics/advsimd.html)
maps `vld3q_u8` to `LD3` of three 16-byte registers, `vqtbl4q_u8` to
`TBL` with a four-register table, and `vst4q_u8` to `ST4` of four
16-byte registers. The table registers must be consecutive. Load the
ordinary alphabet `ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/`
as four contiguous vectors once outside the loop. A four-vector table load
is **not** an `LD4` of that ordinary alphabet: `LD4` would deinterleave it.
The older simdutf source uses an intentionally permuted constant with `LD4`;
the simpler ordinary-table/`LD1` arrangement avoids that extra convention.

By inspection, the unrolled-once body needs 14 architectural instructions
including `LD3`/`ST4`; pointer post-indexing and two or three loop-control
instructions put a simple loop near 64–68 bytes. This estimate excludes
constant setup, entry/exit, padding, capacity handling and JSON quotes.
Instruction count is not a cycle count: structured loads/stores and four-table
`TBL` may execute as multiple internal operations. Actual code size and M1
throughput must come from the implementation and benchmark.

## Why this direction, and alternatives

The [Go 1.25 encoder](https://github.com/golang/go/blob/go1.25.0/src/encoding/base64/base64.go#L133-L180)
processes one three-byte group at a time with scalar alphabet loads. Its
`AppendEncode` already grows the output and encodes directly into unused
capacity, without first clearing a temporary encoded region. The current
`jitEncodeByteSlice` additionally appends a zero-filled region before calling
`Encode`. Reserving the complete quoted result once and writing directly
into its capacity is a separate opportunity from SIMD and should be measured
separately.

Muła and Lemire's [AVX2 paper](https://arxiv.org/abs/1704.00605) establishes
the vectorize-reshuffle-map approach, but its speedups measure Intel AVX2,
not M1. Their [AVX-512 paper, §3.1](https://arxiv.org/html/1910.05109#S3.SS1)
uses two `VPERMB` and one `VPMULTISHIFTQB` for the transformation excluding
loads/stores. The useful lesson is to match the algorithm to available
instructions; those x86 instructions do not translate to three NEON
instructions. ARM64's `LD3`/`ST4` is the appropriate first experiment here.

A compressed 16-byte offset table can replace the full alphabet. It requires
classifying each index into uppercase, lowercase, digits, plus and slash,
then adding the chosen offset. Muła's
[NEON lookup experiments](https://github.com/WojciechMula/base64simd/blob/master/encode/lookup.neon.cpp)
include arithmetic and saturated-subtract/table versions. For this JIT,
the four-register alphabet should be tried first: it maps a vector in one
`TBL`, whereas a compressed table needs several dependent operations and
additional classification constants. A smaller table is not automatically
a smaller or faster loop.

Other candidate experiments, only after a measured baseline:

- Unroll two blocks to expose independent loads and table lookups. Compare
  against one block before considering four or eight; aklomp uses larger
  unrolling, but its choice is not evidence of an M1 optimum.
- Compare structured loads/stores with ordinary loads plus explicit byte
  rearrangement if profiling shows that structure instructions limit
  throughput. This adds instructions and should earn its code size.
- Add a 24-byte/8-lane vector tail. simdutf uses exactly this intermediate
  size before its scalar tail. It can help 24–47-byte values without adding
  an entire second long-input loop.

## Bounds, JSON semantics and JIT integration

The following are recommendations for this repository, rather than claims
made by the cited implementations.

1. Preserve `nil []byte → null` and non-nil empty `[]byte → ""`. Reserve
   `2 + 4 * ceil(n / 3)` bytes with checked size arithmetic, then write both
   quotes and the encoded payload. Keep allocation on the Go slow path.
2. Only enter a 48-byte load when at least 48 source bytes remain and 64
   destination bytes are available. Use the same exact-bound rule for a
   24-byte tail. Do not inherit padded-input assumptions from another codec.
3. Process remaining complete triples with scalar operations. For one final
   byte write two alphabet characters and `==`; for two write three
   characters and `=`. Unused bits must be zero. No newlines or URL alphabet
   belong in JSON's standard Base64 path. The format and padding rules are in
   [RFC 4648 §§3–4](https://www.rfc-editor.org/rfc/rfc4648.html#section-4).
4. Preserve a bounded work budget for a `NOSPLIT` leaf or an unsafe JIT loop,
   as with this repository's string quoting kernel. A convenient input
   chunk is **49,152 bytes**, divisible by both 48 and 3 and producing exactly
   65,536 bytes. Intermediate chunks must not append padding; only the final
   chunk may do so. Returning through a Go wrapper can supply scheduling
   opportunities between chunks.
5. Keep all input/output owners reachable across allocation or Go calls, and
   reload moved pointers after such calls. Do not carry live heap pointers
   solely in unreported JIT registers. Respect the project's Go ABI register
   and stack-map rules when selecting a kernel calling convention.
6. Test source/output overlap according to the public API's actual contract;
   forward Base64 expansion is not inherently safe in-place. Do not treat a
   SIMD load happening to read ahead as overlap support.

Inlining the nil check, output length arithmetic, capacity fast path and
quotes is useful specialization. The NEON transform itself does not depend
on the Go element type once `[]byte` is known. A compact emitted loop can
avoid the Go helper call; alternatively a shared leaf kernel avoids copying
the full padding and long-input machinery into every encoder. Benchmark
these choices separately from the scalar-to-SIMD improvement.

SSA is not required to emit this fixed loop. If many byte-slice fields make
machine code grow, an inline-size budget or shared cold slow paths directly
address duplication. SSA may remove repeated capacity arithmetic or dead
loads, but cannot generally merge side-effecting encode operations into one
without additional transformations. Measure both final code bytes and cold
compile latency before revisiting the earlier SSA experiment.

## Acceptance measurements

Measure the standalone transform and complete JSON encoding independently:

- Sizes 0–8, 15–17, 23–25, 47–49, 95–97, 256, 1 KiB, 4 KiB, 64 KiB,
  and 1 MiB; repeated byte patterns and random payloads.
- Reused output capacity versus fresh allocation; direct `[]byte`, structs
  with many byte-slice fields, and interface-held byte slices.
- Scalar standard-library baseline, scalar direct-capacity variant, shared
  NEON leaf, and emitted/inlined NEON path where available.
- ns/op, input GB/s, bytes and allocations per operation, code bytes per
  compiled type, and uncached compile time. Time cold compilation separately
  from steady-state encoding; do not profile during throughput timing.

Differential tests should compare byte-for-byte with `encoding/base64` and
`encoding/json`, cover every short length and source/destination alignment,
exercise exact capacities and growth, and place source ends next to a guard
page. Cover vector boundaries, work-budget boundaries and one/two-byte tails.
For allocation slow paths, include forced GC/stack growth tests. No M1
speedup, cache-miss or branch-misprediction claim is established by this note.

## Source attribution

The algorithm can be implemented from the format equations and documented
instructions. If translating or copying upstream code, retain the applicable
upstream notices: aklomp's
[BSD-style license](https://github.com/aklomp/base64/blob/v0.5.2/LICENSE)
requires its notices in source and binary redistribution materials;
simdutf offers [Apache 2.0 or MIT](https://github.com/simdutf/simdutf/tree/v7.7.0#license),
and its [MIT text](https://github.com/simdutf/simdutf/blob/v7.7.0/LICENSE-MIT)
requires its copyright and permission notice with copies or substantial
portions. The paper citation alone does not replace a copied implementation's
license notice.
