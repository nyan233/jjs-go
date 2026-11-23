//go:build arm64 && (darwin || linux)

package compiler

import (
	"testing"

	"golang.org/x/sys/unix"
)

func TestARM64QuoteGuardPages(t *testing.T) {
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
		if err := unix.Mprotect(data[2*page:], unix.PROT_NONE); err != nil {
			t.Fatal(err)
		}
		return data[page : 2*page : 2*page]
	}
	src, dst := guarded(), guarded()
	for n := 0; n <= 128; n++ {
		input := src[page-n:]
		for _, last := range []byte{'a', '"', '<', 0, 0xe2, 0xff} {
			for i := range input {
				input[i] = 'a'
			}
			if n != 0 {
				input[n-1] = last
			}
			for capacity := 0; capacity <= min(64, n*6+1); capacity++ {
				output := dst[page-capacity:]
				for i := range output {
					output[i] = 0xcc
				}
				checkQuotePrefix(t, input, output, capacity)
			}
		}
		// Exercise vector validation and partial-rune commits directly against
		// the protected page, including truncated final characters.
		for _, pattern := range []string{"中😀é", "“\u2028\u2029", "\xed\xa0\x80\xf4\x90\x80\x80"} {
			for i := range input {
				input[i] = pattern[i%len(pattern)]
			}
			for capacity := 0; capacity <= min(64, n*6+1); capacity++ {
				output := dst[page-capacity:]
				for i := range output {
					output[i] = 0xcc
				}
				checkQuotePrefix(t, input, output, capacity)
			}
		}
	}
}
