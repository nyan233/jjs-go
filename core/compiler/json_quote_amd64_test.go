//go:build amd64 && !arm64_asm_test

package compiler

import (
	"bytes"
	"strings"
	"testing"
	"unsafe"
)

func checkAMD64QuotePrefix(t *testing.T, source, output []byte, capacity int) {
	t.Helper()
	consumed, written := quoteJSONString(
		unsafe.SliceData(output),
		capacity,
		unsafe.SliceData(source),
		len(source),
	)
	if consumed < 0 || consumed > len(source) || written < 0 || written > capacity {
		t.Fatalf("invalid progress: source=%d capacity=%d consumed=%d written=%d", len(source), capacity, consumed, written)
	}
	if len(source) != 0 && capacity >= 6 && consumed == 0 {
		t.Fatal("encoder made no progress with room for an escaped byte")
	}
	want := referenceQuoted(t, string(source[:consumed]))
	if !bytes.Equal(output[:written], want[1:len(want)-1]) {
		t.Fatalf("prefix mismatch: input=%q cap=%d consumed=%d got=%q want=%q", source, capacity, consumed, output[:written], want[1:len(want)-1])
	}
	for _, c := range output[written:] {
		if c != 0xcc {
			t.Fatal("write beyond reported output")
		}
	}
}

func TestAMD64QuoteBoundsAndAlignment(t *testing.T) {
	values := []string{"", "\u2028\u2029", "\xc2", "\xe0\xa0", "\xf0\x90\x80", "\xff", strings.Repeat("a", 33)}
	for _, c := range quotedCases() {
		values = append(values, c.value)
	}
	for _, value := range values {
		for inputOffset := 0; inputOffset < 16; inputOffset++ {
			source := append(bytes.Repeat([]byte{0x7f}, inputOffset), value...)
			original := bytes.Clone(source)
			for outputOffset := 0; outputOffset < 16; outputOffset++ {
				for _, capacity := range []int{0, 1, 2, 3, 5, 6, 7, 15, 16, 17, 31, 32, len(value)*6 + 1} {
					output := bytes.Repeat([]byte{0xcc}, outputOffset+capacity+16)
					checkAMD64QuotePrefix(t, source[inputOffset:], output[outputOffset:], capacity)
					for _, c := range output[:outputOffset] {
						if c != 0xcc {
							t.Fatal("write before destination")
						}
					}
				}
			}
			if !bytes.Equal(source, original) {
				t.Fatal("modified source")
			}
		}
	}
}

func TestAMD64QuoteChunkBoundaries(t *testing.T) {
	for _, unit := range []string{"x", "\u0080", "中文", "😀", "\u2028\u2029", "\xff", "\n<"} {
		for offset := 0; offset < 4; offset++ {
			value := strings.Repeat("a", (64<<10)-offset) + unit + strings.Repeat(unit, 200)
			want := referenceQuoted(t, value)
			got := appendQuoted(nil, value)
			if !bytes.Equal(got, want) {
				t.Fatalf("chunk boundary mismatch: unit=%q offset=%d", unit, offset)
			}
			dst := make([]byte, 0, len(want))
			got = appendQuoted(dst, value)
			if !bytes.Equal(got, want) || unsafe.SliceData(got) != unsafe.SliceData(dst) {
				t.Fatal("chunk continuation failed to reuse a sufficient output buffer")
			}
		}
	}
	value := strings.Repeat("a", quoteChunkBytes+1)
	dst := make([]byte, 0, len(value)+2)
	got := appendQuoted(dst, value)
	if unsafe.SliceData(got) != unsafe.SliceData(dst) {
		t.Fatal("unnecessary growth after chunk boundary")
	}
	source := bytes.Repeat([]byte{'a'}, 128<<10)
	output := make([]byte, len(source))
	consumed, written := quoteJSONString(unsafe.SliceData(output), len(output), unsafe.SliceData(source), len(source))
	if consumed != 64<<10 || written != consumed {
		t.Fatalf("unexpected bounded progress: %d, %d", consumed, written)
	}
}
