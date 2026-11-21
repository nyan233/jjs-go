//go:build amd64

package compiler

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"
	"unsafe"
)

func TestAVX2FindJSONEscape(t *testing.T) {
	tests := []string{
		"",
		"short",
		strings.Repeat("a", 31),
		strings.Repeat("a", 32),
		strings.Repeat("a", 33),
		strings.Repeat("a", 63) + `"`,
		strings.Repeat("a", 64) + `\`,
		strings.Repeat("a", 95) + "\n",
		"safe prefix < escaped suffix",
		"safe prefix & escaped suffix",
		"中文 UTF-8 text",
	}
	for _, value := range tests {
		got := -1
		if len(value) > 0 {
			got = avx2FindJSONEscape((*byte)(unsafe.Pointer(unsafe.StringData(value))), len(value))
		}
		want := firstJSONSpecial(value)
		if got != want {
			t.Fatalf("value length %d: got %d, want %d", len(value), got, want)
		}
	}
}

func TestAVX2FindJSONStringEnd(t *testing.T) {
	tests := []string{
		"",
		"short",
		strings.Repeat("a", 31),
		strings.Repeat("a", 32),
		strings.Repeat("a", 33),
		strings.Repeat("a", 63) + `"`,
		strings.Repeat("a", 64) + `\`,
		strings.Repeat("a", 95) + "\n",
		"safe <html> & punctuation",
		"中文 UTF-8 text",
	}
	for _, value := range tests {
		got := -1
		if len(value) > 0 {
			got = avx2FindJSONStringEnd((*byte)(unsafe.Pointer(unsafe.StringData(value))), len(value))
		}
		want := firstJSONStringEnd(value)
		if got != want {
			t.Fatalf("value length %d: got %d, want %d", len(value), got, want)
		}
	}
}

func TestSSEFindJSONEscape(t *testing.T) {
	escapeBytes := []byte{0x00, '"', '\\', '<', '>', '&', 0x1f, 0x80, 0xff}
	for length := 0; length <= 255; length++ {
		safe := bytes.Repeat([]byte{'a'}, length)
		if got := sseFindJSONEscape((*byte)(unsafe.SliceData(safe)), len(safe)); got != -1 {
			t.Fatalf("SSE escape length %d: got %d, want -1", length, got)
		}
		for _, special := range escapeBytes {
			for position := 0; position < length; position++ {
				value := bytes.Clone(safe)
				value[position] = special
				if got := sseFindJSONEscape((*byte)(unsafe.SliceData(value)), len(value)); got != position {
					t.Fatalf("SSE escape length %d position %d byte %#x: got %d", length, position, special, got)
				}
			}
		}
	}
}

func TestAVX2QuotedMatchesEncodingJSON(t *testing.T) {
	values := []string{
		strings.Repeat("a", 1024),
		strings.Repeat("中文", 300),
		strings.Repeat(`quote"slash\`, 50),
		strings.Repeat("<html>&", 100),
		strings.Repeat("control\x01", 50),
		strings.Repeat("a", 127) + "\u2028" + strings.Repeat("b", 128),
	}
	for length := 0; length <= 300; length++ {
		values = append(values, strings.Repeat("a", length))
		if length > 0 {
			values = append(values,
				strings.Repeat("a", length/2)+`"`+strings.Repeat("b", length-length/2-1),
				strings.Repeat("a", length/2)+"\x01"+strings.Repeat("b", length-length/2-1),
			)
		}
	}
	for _, value := range values {
		got := appendQuoted(nil, value)
		want, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != string(want) {
			t.Fatalf("quoted output mismatch for length %d", len(value))
		}
	}
}

func firstJSONSpecial(value string) int {
	for i := 0; i < len(value); {
		c := value[i]
		if c < 0x20 || c >= 0x80 || c == '"' || c == '\\' || c == '<' || c == '>' || c == '&' {
			return i
		}
		if c < utf8.RuneSelf {
			i++
			continue
		}
		_, size := utf8.DecodeRuneInString(value[i:])
		i += size
	}
	return -1
}

func firstJSONStringEnd(value string) int {
	for i := 0; i < len(value); i++ {
		c := value[i]
		if c < 0x20 || c >= utf8.RuneSelf || c == '"' || c == '\\' {
			return i
		}
	}
	return -1
}

func BenchmarkAVX2AppendQuotedLongASCII(b *testing.B) {
	value := strings.Repeat("abcdefghijklmnopqrstuvwxyz", 256)
	b.SetBytes(int64(len(value)))
	for i := 0; i < b.N; i++ {
		_ = appendQuoted(nil, value)
	}
}

func BenchmarkShortJSONScan(b *testing.B) {
	for _, length := range []int{8, 16, 32, 64, 128, 255} {
		value := bytes.Repeat([]byte{'a'}, length)
		b.Run("SSEEscape/"+strconv.Itoa(length), func(b *testing.B) {
			ptr := (*byte)(unsafe.SliceData(value))
			for i := 0; i < b.N; i++ {
				_ = sseFindJSONEscape(ptr, len(value))
			}
		})
	}
}
