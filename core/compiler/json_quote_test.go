package compiler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"
	"testing"
)

func quotedCases() []struct{ name, value string } {
	cases := []struct{ name, value string }{
		{"Unicode", strings.Repeat("中文测试", 64)},
		{"UnicodeThenASCII", "中文" + strings.Repeat("abcdefgh", 128)},
		{"Emoji", strings.Repeat("😀🌍", 64)},
		{"Escapes", strings.Repeat("a\n\"\\中文", 32)},
		{"HTML", strings.Repeat("<p>&</p>", 64)},
		{"Controls", strings.Repeat("\x00\x01\b\f\n\r\t\x1f", 64)},
		{"InvalidUTF8", strings.Repeat("a\xff\xc0\xe0\x80\x80", 64)},
		{"LateEscape", strings.Repeat("abcdefgh", 128) + "\n"},
	}
	for _, n := range []int{0, 4, 8, 16, 32, 64, 256, 1024} {
		cases = append(cases, struct{ name, value string }{fmt.Sprintf("ASCII%d", n), strings.Repeat("a", n)})
	}
	for _, n := range []int{8, 16, 32, 64} {
		cases = append(cases, struct{ name, value string }{fmt.Sprintf("UTF8Bytes%d", n), strings.Repeat("é", n/2)})
	}
	return cases
}

func TestAppendQuotedDifferential(t *testing.T) {
	values := []string{"", "\u2028\u2029", "\ufffd", "\xc2", "\xe0\xa0", "\xf0\x90\x80", "\xed\xa0\x80", "\xf4\x90\x80\x80", "\xc2\x80", "\xdf\xbf", "\xe0\xa0\x80", "\xed\x9f\xbf", "\xf0\x90\x80\x80", "\xf4\x8f\xbf\xbf"}
	for _, c := range quotedCases() {
		values = append(values, c.value)
	}
	for c := 0; c < 256; c++ {
		for _, n := range []int{0, 1, 7, 8, 15, 16, 17, 31, 32, 33, 63, 64} {
			values = append(values, strings.Repeat("a", n)+string([]byte{byte(c)})+"tail")
		}
	}
	rng := rand.New(rand.NewSource(1))
	for n := 0; n < 2000; n++ {
		data := make([]byte, rng.Intn(200))
		rng.Read(data)
		values = append(values, string(data))
	}
	for _, value := range values {
		want := referenceQuoted(t, value)
		for _, extra := range []int{0, 1, 7, len(value) + 2, 6*len(value) + 2} {
			dst := make([]byte, 3, 3+extra)
			copy(dst, "pre")
			got := appendQuoted(dst, value)
			if !bytes.Equal(got[:3], []byte("pre")) || !bytes.Equal(got[3:], want) {
				t.Fatalf("value=%q extra=%d got=%q want=%q", value, extra, got, want)
			}
		}
	}
}

var quotedBenchmarkResult []byte

func BenchmarkAppendQuoted(b *testing.B) {
	for _, c := range quotedCases() {
		b.Run(c.name, func(b *testing.B) {
			for _, reuse := range []bool{true, false} {
				name := "Allocate"
				if reuse {
					name = "Reuse"
				}
				b.Run(name, func(b *testing.B) {
					var dst []byte
					if reuse {
						dst = make([]byte, 0, len(c.value)*6+2)
					}
					b.ReportAllocs()
					b.SetBytes(int64(len(c.value)))
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						quotedBenchmarkResult = appendQuoted(dst[:0], c.value)
					}
				})
			}
		})
	}
}

func FuzzAppendQuoted(f *testing.F) {
	for _, c := range quotedCases() {
		f.Add(c.value, uint8(0))
	}
	f.Fuzz(func(t *testing.T, value string, capacity uint8) {
		dst := make([]byte, 3, 3+int(capacity))
		copy(dst, "pre")
		got := appendQuoted(dst, value)
		want := referenceQuoted(t, value)
		if !bytes.Equal(got[:3], []byte("pre")) || !bytes.Equal(got[3:], want) {
			t.Fatalf("input=%q cap=%d got=%q want=%q", value, capacity, got, want)
		}
	})
}

// Keep jjs-go's existing byte representation stable across Go releases. For
// example, Go 1.21 emits \u0008 for backspace while later versions use \b;
// Go 1.27 may emit the UTF-8 replacement rune instead of an escaped \ufffd.
// Check the Go reference against encoding/json semantically, then require the
// native encoder to match that reference byte-for-byte (including HTML escapes).
func referenceQuoted(t testing.TB, value string) []byte {
	t.Helper()
	want := appendQuotedGo(nil, value)
	standard, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var gotValue, standardValue string
	if err := json.Unmarshal(want, &gotValue); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(standard, &standardValue); err != nil {
		t.Fatal(err)
	}
	if gotValue != standardValue {
		t.Fatalf("reference differs from encoding/json for %q", value)
	}
	return want
}

func TestAppendQuotedEscapeSpelling(t *testing.T) {
	for _, test := range []struct{ value, want string }{
		{"\b\f\n\r\t", `"\b\f\n\r\t"`},
		{"<>&", `"\u003c\u003e\u0026"`},
		{"\x00\x1f", `"\u0000\u001f"`},
		{"\u2028\u2029", `"\u2028\u2029"`},
		{"\xff", `"\ufffd"`},
		{"\ufffd", `"�"`},
		{"\\\"", `"\\\""`},
	} {
		if got := string(appendQuoted(nil, test.value)); got != test.want {
			t.Fatalf("input=%q got=%s want=%s", test.value, got, test.want)
		}
	}
}
