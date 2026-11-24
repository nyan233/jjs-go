//go:build amd64 && (darwin || linux) && !arm64_asm_test

package compiler

import (
	"bytes"
	"testing"
	"unsafe"

	"golang.org/x/sys/unix"
)

func TestAMD64QuoteGuardPages(t *testing.T) {
	page := unix.Getpagesize()
	guarded := func() []byte {
		data, err := unix.Mmap(-1, 0, page*3, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_ANON|unix.MAP_PRIVATE)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := unix.Munmap(data); err != nil {
				t.Error(err)
			}
		})
		if err := unix.Mprotect(data[:page], unix.PROT_NONE); err != nil {
			t.Fatal(err)
		}
		if err := unix.Mprotect(data[page*2:], unix.PROT_NONE); err != nil {
			t.Fatal(err)
		}
		return data[page : page*2 : page*2]
	}
	src, dst := guarded(), guarded()
	for n := 0; n <= 256; n++ {
		source := src[page-n:]
		for i := range source {
			source[i] = byte(i*73 + n)
		}
		want := referenceQuoted(t, string(source))
		var output []byte
		if len(want) > 2 {
			output = dst[page-(len(want)-2):]
		}
		consumed, written := quoteJSONString(unsafe.SliceData(output), len(output), unsafe.SliceData(source), len(source))
		if consumed != len(source) {
			t.Fatalf("length %d consumed %d", n, consumed)
		}
		got := append([]byte{'"'}, output[:written]...)
		got = append(got, '"')
		if !bytes.Equal(got, want) {
			t.Fatalf("length %d mismatch: got %q want %q", n, got, want)
		}
	}
}
