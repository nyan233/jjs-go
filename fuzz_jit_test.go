//go:build jitfuzz

package jjs_go

import (
	"bytes"
	"encoding/json"
	"testing"
	"unicode/utf8"
)

func FuzzJITMarshalString(f *testing.F) {
	for _, seed := range []string{"", "plain", "\x00\n\t<>&\\\"", "中文😀", "\u2028\u2029"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, value string) {
		got, err := Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		want, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("got %q, want %q", got, want)
		}
		var decoded string
		if err := Unmarshal(got, &decoded); err != nil {
			t.Fatal(err)
		}
		if utf8.ValidString(value) && decoded != value {
			t.Fatalf("round trip mismatch: got %q, want %q", decoded, value)
		}
	})
}

func FuzzJITUnmarshalString(f *testing.F) {
	for _, seed := range []string{`""`, `"plain"`, `"hello\nworld"`, `"\u4e16\u754c"`, `null`, `123`, `"unterminated`} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input string) {
		var got string
		gotErr := Unmarshal([]byte(input), &got)
		var want string
		wantErr := json.Unmarshal([]byte(input), &want)
		if (gotErr == nil) != (wantErr == nil) {
			t.Fatalf("error mismatch for %q: jit=%v stdlib=%v", input, gotErr, wantErr)
		}
		if gotErr == nil && got != want {
			t.Fatalf("value mismatch: got %q, want %q", got, want)
		}
	})
}
