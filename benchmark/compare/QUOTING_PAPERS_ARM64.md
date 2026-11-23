# Paper-guided ARM64 string quoting

Research date: 2026-11-23. Starting implementation: `2426eeb` on Apple M1 Pro.
This note separates published evidence, proposed adaptations and local
measurements. The starting kernel already combines ASCII
classification, copying and scalar UTF-8 validation. Its remaining opportunity
is to copy valid non-ASCII blocks without visiting each rune in scalar code.

## Primary sources and applicability

1. **Keiser and Lemire, _Validating UTF-8 In Less Than One Instruction Per
   Byte_**, Software: Practice and Experience 51(5), 2021; read §§4–7 and 9 of
   [the paper](https://arxiv.org/pdf/2010.03090v5), and the authors' maintained
   [lookup4 implementation](https://github.com/simdutf/simdutf/blob/master/src/generic/utf8_validation/utf8_lookup4_algorithm.h).
   Three 16-byte lookup tables classify adjacent-byte errors. Shifted previous
   bytes supply continuation-position checks, and a separate check detects
   unfinished final code points. NEON `EXT`, byte shifts and `TBL` fit the method
   directly. The paper's measurements use AVX2 on Skylake and AMD Rome, not M1;
   its throughput is for validation, without JSON escaping or output writes.
   Its branch-prediction experiment also shows how repeatedly benchmarking
   short identical strings can favor scalar code. The useful contribution here
   is the validator design, not a transferable speedup factor.

2. **Langdale and Lemire, _Parsing Gigabytes of JSON per Second_**, VLDB Journal
   28, 2019; read §§3.1.2, 3.1.4 and the optimization discussion in
   [the paper](https://arxiv.org/pdf/1902.08318v7).
   Two nibble lookups classify byte sets, while bitsets compactly represent
   exceptional positions. Trailing-zero extraction visits those positions
   without inspecting every ordinary byte. Our existing ASCII classifier
   already uses the first technique. The second motivates a sparse-escape
   experiment, but the paper parses JSON: recognizing quotes and producing
   structural indexes does not solve variable-length escaped output. Its
   carry-less-multiply quote-parity trick is unnecessary when quoting one known
   string. simdjson's current [buffer documentation](https://github.com/simdjson/simdjson/blob/master/doc/basics.md)
   also permits accesses into padding; our kernel must retain exact input and
   output bounds rather than inherit that contract.

3. **Cai's UTF-8 range validator**, primary
   [algorithm explanation](https://github.com/cyb70289/utf8/blob/master/README.md)
   and [NEON implementation](https://github.com/cyb70289/utf8/blob/master/range-neon.c).
   Lead-byte classes generate allowed byte ranges; `EXT` and saturating
   subtraction propagate expected continuation positions, and lookup tables
   select minimum/maximum values. A further table handles E0/ED/F0/F4. The
   implementation carries previous-block state and revisits a short suffix
   with its scalar validator. Its ARM performance claim compares an older
   Lemire validator, not lookup4, and identifies neither an M1 nor an M1 Pro.
   In the inspected loop, range requires five table lookup operations versus
   lookup4's three, with more resident constants. That makes lookup4 the first
   experiment here; instruction count alone does not establish the winner.

4. **Lemire and Muła, _Transcoding Billions of Unicode Characters per Second
   with SIMD Instructions_**, Software: Practice and Experience 52(2), 2022;
   read the table-based algorithms, UTF-16-to-UTF-8 path and §§6.2, 6.6–6.7 in
   [the paper](https://arxiv.org/pdf/2109.10433v3).
   It demonstrates variable output lengths using byte permutations selected
   by compact masks, including measurements on Apple M1 with Apple LLVM 12
   and `-O3`. Its UTF-16-to-UTF-8 tables total 8.5 KiB; it keeps scalar handling
   for cases where full vectorization introduces unattractive compromises.
   The transferable idea is to form fixed-width candidate output and compact
   it using `TBL`. JSON quoting has 1/2/6-byte outputs, a different expansion
   pattern. This paper supplies a credible design direction, not a ready-made
   JSON escaping algorithm or evidence that a large shuffle table beats our
   packed scalar escape table.

## Proposed experiments

These are deductions for this kernel, not claims made by the papers.

**First: fused UTF-8 validation and copying.** Retain the current cheap ASCII
path. On a sufficiently long non-ASCII block, use lookup4 on bytes already in
NEON registers. Load its constants only when this path is needed. Reject a
candidate block before writing if it contains invalid UTF-8 or a required JSON
escape. Keep the scalar path for rejection: a boolean validator cannot itself
implement Go's one-invalid-byte-at-a-time replacement semantics.

A bounded, independent-block design avoids maintaining validation state through
scalar escapes and capacity returns:

1. Enter at a rune boundary and load 16 bytes only when both buffers have room.
2. Set previous-input bytes to zero, run lookup4's block checks, and reject any
   error. Do not yet require the final rune to be complete.
3. Select a complete-rune prefix. Starting at length 16, use the first matching
   condition: `b[15] >= 0xc0` gives 15; otherwise `b[14] >= 0xe0` gives 14;
   otherwise `b[13] >= 0xf0` gives 13. Otherwise retain 16.
4. Check ASCII escapes and U+2028/U+2029 before committing the prefix. Keep any
   unfinished trailing rune for the next iteration. Copy exactly the committed
   length, with no padded source load or speculative destination overwrite.

The prefix rule is valid only after successful block validation and with the
entry-boundary invariant. Errors in an uncommitted suffix may conservatively
reject a whole block; this only costs time. Cross-boundary U+2028/U+2029 remains
unconsumed until its full encoding is available. A simpler first experiment
can reject every block containing E2, but that also penalizes ordinary curly
quotes and other punctuation; measure before retaining that shortcut.

**Second: sparse escapes.** The existing scalar window processes up to 16 input
bytes after one special hit. Test extracting an exception mask only on a hit,
bulk-copying its ordinary prefix, then emitting the escape. Keep the zero-hit
path as a simple reduction. Compare against dense escapes: mask extraction,
prefix stores and repeated dispatch can outweigh saved scalar work there.

**Third: restricted vector expansion.** If profiles still justify it, prototype
1/2-byte output blocks using a small shuffle table, retaining scalar fallback
for six-byte escapes and invalid UTF-8. Full 1/2/6-byte expansion increases
shuffle cases, capacity handling and table footprint. It should earn its place
against the existing packed stores, particularly on short strings and cold
tables. Baseline 128-bit NEON should not be treated as having AVX-512 byte
compression or wider SVE facilities.

## Acceptance measurements

Compare against `2426eeb`, not the earlier Go implementation. Use alternating
baseline/candidate runs and separate reused-output from allocation cases.
Include short ASCII, long ASCII, homogeneous and mixed-width Unicode, sparse
and dense escapes, punctuation containing E2, invalid byte streams, and inputs
rotated through a varied corpus. Include complete documents because additional
constants and branches may cost more than they save on short field names.

Boundary regressions must cover every alignment, exact capacity, rune splits
at 16-byte and work-budget boundaries, truncated sequences, E0/ED/F0/F4 range
violations, and split U+2028/U+2029. Preserve guard-page checks and differential
comparison with `encoding/json`. Record code/table growth. CPU profiles can
locate hot routines, but cannot establish cache-miss or branch-misprediction
rates; report those only when hardware-counter measurements are available.

## Implemented adaptation

The first experiment is implemented in `core/compiler/json_quote_arm64.s`.
The three lookup4 tables add 48 declared read-only bytes; their header retains
the simdutf MIT notice. UTF-8 constants are initialized on demand once per
bounded assembly call. No dependency, cgo call or JIT pass is added.

The kernel keeps the 8/16-byte ASCII path. For a non-ASCII 16-byte block, it
first excludes ASCII escapes, then validates adjacent-byte classes and required
continuation positions. It separately detects E2 80 A8/A9. Successful blocks
commit only complete runes; 13–15-byte prefixes use two overlapping 8-byte
stores that touch exactly the committed interval. Rejected blocks reach the
existing scalar window before any output is written.

The [C intrinsic experiment](experiments/quote-arm64/utf8_lookup4.c) and retained
[Clang assembly](experiments/quote-arm64/clang-utf8-o3.s.txt) corroborate the
`EXT`/`TBL`/comparison/XOR formulation. Apple Clang 21 with `-O3 -mcpu=apple-m1`
uses immediate vector constants. Screening versions with immediate constants
and/or maximum reduction did not improve the overall tradeoff: on the same
test harness, the repeated invalid-byte case measured about 732 ns with the
immediate-only version versus 635 ns with the retained version. Increasing
the rejection window to 64 bytes only reduced it to about 720 ns and was also
rejected. Fewer instructions did not translate into less time here; these
probes do not identify the hardware cause. The retained kernel uses the
original constant setup and widening-sum reduction.

Sparse-escape position extraction and full SIMD output expansion remain
proposals. The implemented validator has a small fixed table footprint and
targets the demonstrated non-ASCII bottleneck; the papers do not establish a
benefit for every JSON workload.

## Correctness checks

- Existing differential tests, source/destination alignment and exact-capacity
  checks, and bounded 64 KiB call continuation tests pass.
- Added every adjacent byte pair at five lane positions and every valid
  Unicode scalar at varying positions, compared byte-for-byte with the prior
  Go quoting implementation. That reference is also checked semantically
  against `encoding/json` to tolerate Go-version escape-spelling differences.
- Added explicit malformed/truncated sequences and U+2028/U+2029 across vector
  boundaries. Protected-page tests now include valid and invalid Unicode runs
  and every output capacity up to 64 bytes.
- `go test ./...`, `go test -race ./...`, comparison-module tests, ARM64 assembly
  tests and `go build ./...` pass. A 30-second fuzz run completed 300,746 inputs
  without a failure.
- `go vet ./...` still reports pre-existing JIT/unsafe and duplicate-tag
  diagnostics; it is not a passing check. The untouched baseline emits the
  same diagnostics.
