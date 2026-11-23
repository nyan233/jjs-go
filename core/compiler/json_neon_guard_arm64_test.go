//go:build arm64 && (darwin || linux)

package compiler

import (
	"testing"
	"unsafe"

	"golang.org/x/sys/unix"
)

func TestARM64NEONPageBoundary(t *testing.T) {
	page := unix.Getpagesize()
	data, err := unix.Mmap(-1, 0, page*2, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_ANON|unix.MAP_PRIVATE)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := unix.Munmap(data); err != nil {
			t.Error(err)
		}
	})
	if err := unix.Mprotect(data[page:], unix.PROT_NONE); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < page; i++ {
		data[i] = 'a'
	}
	for length := 0; length < 300; length++ {
		ptr := (*byte)(unsafe.Add(unsafe.Pointer(unsafe.SliceData(data)), page-length))
		for mode := uint64(0); mode < 2; mode++ {
			if got := scanJSONSpecialNEON(ptr, length, mode); got != -1 {
				t.Fatalf("length %d mode %d: got %d", length, mode, got)
			}
		}
	}
	data[page-1] = '"'
	for length := 1; length < 300; length++ {
		ptr := &data[page-length]
		for mode := uint64(0); mode < 2; mode++ {
			if got := scanJSONSpecialNEON(ptr, length, mode); got != length-1 {
				t.Fatalf("length %d mode %d: got %d", length, mode, got)
			}
		}
	}
}
