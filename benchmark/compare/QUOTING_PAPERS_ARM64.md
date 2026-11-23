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

## M1 Pro measurements

Baseline: `2426eeb`; implementation: `fcda8e4`. Apple M1 Pro, macOS 27.0.1,
Go 1.25.0 darwin/arm64; `GOMAXPROCS=10`, `-test.cpu=10` and
`SONIC_NO_ASYNC_GC=1`. Baseline binaries use the same added benchmark harness.
All binaries were built before timing; no builds, tests or profilers ran
concurrently. Each before/after series has five pairs, alternating run order.
String/hotpath cases run for 200 ms per sample; document cases for 500 ms.
These are median elapsed times, not cycle counts or confidence intervals.

Raw samples, including discarded screening variants, are in
[results-darwin-arm64-quoting-papers.txt](results-darwin-arm64-quoting-papers.txt).
The standalone C prefix experiment is not included in these API timings.
The Go quoting wrapper and allocation policy are unchanged. The native routine
in the measured binary grows from 832 to 1,120 bytes; the added lookup tables
contain 48 declared bytes. There is no extra runtime compilation stage.

### String quoting with reused output

All cases below remain at zero allocations per operation. The corpus benchmarks
rotate through 1,024 deterministic strings of about 256 bytes each. UTF8 mixes
ASCII, accented Latin, Greek, CJK, emoji and curly quotes; Mixed additionally
contains escapes, HTML, U+2028 and invalid bytes.

| Case | Before ns/op | After ns/op | Time change | Faster pairs |
| --- | ---: | ---: | ---: | ---: |
| Unicode (768 B) | 643.000 | 166.100 | -74.2% | 5/5 |
| Emoji (512 B) | 357.700 | 108.300 | -69.7% | 5/5 |
| Unicode then ASCII | 69.140 | 60.770 | -12.1% | 5/5 |
| UTF-8 (8 B) | 13.100 | 13.060 | -0.3% | 4/5 |
| UTF-8 (16 B) | 17.490 | 9.569 | -45.3% | 5/5 |
| UTF-8 (32 B) | 29.690 | 12.730 | -57.1% | 5/5 |
| ASCII (8 B) | 7.344 | 7.340 | -0.1% | 4/5 |
| ASCII (16 B) | 6.727 | 6.658 | -1.0% | 3/5 |
| ASCII (1 KiB) | 54.400 | 54.590 | +0.3% | 3/5 |
| Escapes | 420.100 | 419.000 | -0.3% | 2/5 |
| HTML | 638.100 | 646.500 | +1.3% | 0/5 |
| Controls | 926.000 | 932.200 | +0.7% | 1/5 |
| Invalid UTF-8 | 613.500 | 636.100 | +3.7% | 0/5 |
| Late escape | 57.140 | 57.310 | +0.3% | 2/5 |
| Rotating UTF8 corpus | 536.300 | 138.100 | -74.2% | 5/5 |
| Rotating Mixed corpus | 862.400 | 872.200 | +1.1% | 1/5 |

The improvement is concentrated in valid non-ASCII blocks. The 3.7% regression
on dense invalid bytes is a real retained tradeoff across all five pairs:
failed vector validation does extra work before scalar replacement. Dense HTML
is 1.3% slower and the mixed corpus is 1.1% slower; long ASCII is essentially
unchanged. The validator does not make variable-length escaping universally
faster.

With allocation included, Unicode moves from 737.3 to 257.5 ns (-65.1%) and
emoji from 424.7 to 171.0 ns (-59.7%). Both retain one allocation. Other
allocation-heavy medians are noisier: the invalid-input candidate series has a
91.9% sample span, so its lower allocation-case median is not evidence that
invalid-byte processing improved. No samples were removed.

### Complete documents and public string API

| Case | Before ns/op | After ns/op | Time change | Faster pairs |
| --- | ---: | ---: | ---: | ---: |
| Binding / Small | 651.0 | 646.2 | -0.7% | 3/5 |
| Binding / Medium | 5,354.0 | 5,376.0 | +0.4% | 2/5 |
| Binding / Large | 216,365.0 | 188,300.0 | -13.0% | 5/5 |
| Generic / Small | 1,901.0 | 1,897.0 | -0.2% | 4/5 |
| Generic / Medium | 19,742.0 | 20,069.0 | +1.7% | 2/5 |
| Generic / Large | 987,846.0 | 939,374.0 | -4.9% | 5/5 |
| UnorderedGeneric / Small | 1,868.0 | 1,873.0 | +0.3% | 3/5 |
| UnorderedGeneric / Medium | 19,716.0 | 19,939.0 | +1.1% | 1/5 |
| UnorderedGeneric / Large | 983,960.0 | 944,154.0 | -4.0% | 5/5 |

Binding Large improves in all five pairs; the before/after sample spans are
6.6%/2.7%. Large generic documents improve about 4–5%. Small/Medium changes are
roughly -0.7% to +1.7%; the UnorderedGeneric Small candidate has a 36.4% span,
so small percentage differences should not be overinterpreted.

Whole-document allocation counts are unchanged: Binding Small/Medium/Large
uses 2/2/210 and Generic/UnorderedGeneric uses 11/77/2,529. Minor large-case B/op
variation comes from amortized runtime/pool behavior.

The public Unicode string encode benchmark moves from 790.7 to 290.4 ns
(-63.3%). Public ASCII encode is essentially unchanged, while the escape-heavy
encode case is 2.3% slower. Decode-only controls move between -0.8% and -4.0%
with no decoding change, illustrating environmental/code-layout noise; this
work does not claim a decoding improvement.

### Comparison with other libraries

Three 500 ms samples per case, median ns/op, goccy v0.11.0 and Sonic v1.15.5.
These are a separate run from the paired table, so its jjs-go absolute times
can differ. Binding uses the repository's existing configurations.
UnorderedGeneric disables map sorting for all libraries and retains HTML
escaping and string validation. Default Generic measurements also appear in
the raw output; differing sorting defaults make them unsuitable for a blanket
library ranking.

| Case | jjs-go ns/op | goccy ns/op | Sonic ns/op |
| --- | ---: | ---: | ---: |
| Binding / Small | 634.8 | 561.1 | 1,594.0 |
| Binding / Medium | 4,802.0 | 3,998.0 | 16,647.0 |
| Binding / Large | 173,364.0 | 218,372.0 | 491,263.0 |
| UnorderedGeneric / Small | 1,867.0 | 1,447.0 | 4,070.0 |
| UnorderedGeneric / Medium | 19,949.0 | 15,225.0 | 43,043.0 |
| UnorderedGeneric / Large | 934,820.0 | 856,184.0 | 1,918,091.0 |

Binding Large is about 20.6% faster than goccy in this run. jjs-go still trails
goccy for Binding Small/Medium and unordered generic documents; this change
does not establish an across-the-board lead. These ARM64 results do not replace
the historical amd64 README measurements.

No cache-miss or branch-misprediction rates were measured: hardware-counter
access is unavailable in this environment. The SIMD gains above are measured
API time; neither the paper's instructions-per-byte figure nor another CPU's
throughput is substituted for a local measurement.

### CI

[Implementation CI for fcda8e4](https://github.com/nyan233/jjs-go/actions/runs/45137638278)
passed all 15 jobs: Windows amd64 and Ubuntu on Go 1.22/1.25, native macOS,
ARM64 cross-compilation and QEMU, and the Go 1.21–1.27 compiler/loader matrix.
