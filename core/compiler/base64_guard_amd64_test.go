//go:build amd64 && (darwin || linux)

package compiler

import (
	"encoding/base64"
	"testing"

	"golang.org/x/sys/unix"
)

func TestAMD64Base64GuardPages(t *testing.T) {
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
		input := src[page-n:]
		for i := range input {
			input[i] = byte(i*101 + n)
		}
		want := base64.StdEncoding.EncodeToString(input)
		output := dst[page-len(want):]
		encodeBase64(output, input)
		if string(output) != want {
			t.Fatalf("length %d: got %q, want %q", n, output, want)
		}
	}
}
