package compiler

import (
	"bytes"
	"math/rand"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestAppendQuotedUTF8BytePairs(t *testing.T) {
	// Exercise every adjacent-byte combination, including pairs crossing a
	// vector boundary, surrounded by text that can enter the UTF-8 fast path.
	for _, offset := range []int{0, 7, 13, 14, 15} {
		value := []byte(strings.Repeat("a", offset) + "xx" + strings.Repeat("中😀é", 4))
		for pair := 0; pair < 1<<16; pair++ {
			value[offset], value[offset+1] = byte(pair>>8), byte(pair)
			got := appendQuoted(nil, string(value))
			want := appendQuotedGo(nil, string(value))
			if !bytes.Equal(got, want) {
				t.Fatalf("pair=%04x offset=%d got=%q want=%q", pair, offset, got, want)
			}
		}
	}
}

func TestAppendQuotedAllUnicodeScalars(t *testing.T) {
	// Check the complete valid Unicode range at varying SIMD lane positions.
	// Mixed-width neighbors force both complete and incomplete block tails.
	for r := rune(0); r <= utf8.MaxRune; r++ {
		if 0xd800 <= r && r <= 0xdfff {
			continue
		}
		value := strings.Repeat("a", int(r&15)) + string(r) + "é中文😀" + string(r) + "é中文😀"
		got := appendQuoted(nil, value)
		want := appendQuotedGo(nil, value)
		if !bytes.Equal(got, want) {
			t.Fatalf("rune=%U got=%q want=%q", r, got, want)
		}
	}
}

func TestAppendQuotedUTF8VectorBoundaries(t *testing.T) {
	units := []string{
		"\u2028", "\u2029", "“”", "\ufffd", "é", "中文", "😀",
		"\xc0\x80", "\xc1\xbf", "\xe0\x9f\xbf", "\xed\xa0\x80",
		"\xf0\x8f\xbf\xbf", "\xf4\x90\x80\x80", "\xf5\x80\x80\x80",
		"\x80", "\xc2", "\xe0\xa0", "\xf0\x90\x80", "\xff",
		"\xe2\x80<", "\xf0\x90\"\x80", "\xed\x9f\\",
	}
	for _, unit := range units {
		for offset := 0; offset < 48; offset++ {
			for _, suffix := range []string{"", "a", "\x80", strings.Repeat("é😀中", 8)} {
				value := strings.Repeat("a", offset) + unit + suffix
				want := referenceQuoted(t, value)
				// Exact output capacity checks that no speculative store or
				// unnecessary allocation is hidden by excess capacity.
				dst := make([]byte, 0, len(want))
				got := appendQuoted(dst, value)
				if !bytes.Equal(got, want) || &got[0] != &dst[:1][0] {
					t.Fatalf("unit=%q offset=%d suffix=%q got=%q want=%q", unit, offset, suffix, got, want)
				}
			}
		}
	}
}

func BenchmarkAppendQuotedCorpus(b *testing.B) {
	for _, mixed := range []bool{false, true} {
		name := "UTF8"
		units := []string{"a", "abc", "é", "中文", "😀", "“”", "日本語", "αβ"}
		if mixed {
			name = "Mixed"
			units = append(units, "\n", "<", "\"", "\u2028", "\xff")
		}
		b.Run(name, func(b *testing.B) {
			rng := rand.New(rand.NewSource(42))
			values := make([]string, 1024)
			total := 0
			for i := range values {
				var value strings.Builder
				for value.Len() < 256 {
					value.WriteString(units[rng.Intn(len(units))])
				}
				values[i] = value.String()
				total += len(values[i])
			}
			dst := make([]byte, 0, 6*272+2)
			b.SetBytes(int64(total / len(values)))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				quotedBenchmarkResult = appendQuoted(dst[:0], values[i&(len(values)-1)])
			}
		})
	}
}
