//go:build windows

package jitloader

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

func mmapExecutable(code []byte) ([]byte, uintptr) {
	size := uintptr((len(code) + 4095) &^ 4095)
	address, err := windows.VirtualAlloc(0, size, windows.MEM_COMMIT|windows.MEM_RESERVE, windows.PAGE_READWRITE)
	if err != nil {
		panic("jitloader: VirtualAlloc: " + err.Error())
	}
	mapping := unsafe.Slice((*byte)(unsafe.Pointer(address)), int(size))
	copy(mapping[:len(code)], code)
	var oldProtect uint32
	if err := windows.VirtualProtect(address, size, windows.PAGE_EXECUTE_READ, &oldProtect); err != nil {
		_ = windows.VirtualFree(address, 0, windows.MEM_RELEASE)
		panic("jitloader: VirtualProtect: " + err.Error())
	}
	return mapping, uintptr(address)
}
