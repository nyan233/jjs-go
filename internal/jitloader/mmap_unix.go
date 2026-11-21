//go:build !windows

package jitloader

import (
	"os"
	"unsafe"

	"golang.org/x/sys/unix"
)

func mmapExecutable(code []byte) ([]byte, uintptr) {
	size := os.Getpagesize()
	mappedSize := (len(code) + size - 1) / size * size
	mapping, err := unix.Mmap(-1, 0, mappedSize, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_ANON|unix.MAP_PRIVATE)
	if err != nil {
		panic("jitloader: mmap: " + err.Error())
	}
	copy(mapping[:len(code)], code)
	if err := unix.Mprotect(mapping, unix.PROT_READ|unix.PROT_EXEC); err != nil {
		_ = unix.Munmap(mapping)
		panic("jitloader: mprotect: " + err.Error())
	}
	return mapping, uintptr(unsafe.Pointer(&mapping[0]))
}
