package sys

import (
	"github.com/nyan233/jjs-go/internal/asm"
	"golang.org/x/sys/unix"
	"unsafe"
)

func ModifyPermissions2Exec(code []byte) []byte {
	if len(code) == 0 {
		return []byte{}
	}
	size := pageSize(uintptr(len(code)))
	ptr := VMalloc(size)
	newSpace := unsafe.Slice((*byte)(ptr), int(size))[:len(code)]
	asm.RtMemoryCopy(ptr, unsafe.Pointer(&code[0]), uintptr(len(code)))
	err := unix.Mprotect(newSpace, unix.PROT_READ|unix.PROT_EXEC)
	if err != nil {
		panic(err)
	}
	return newSpace
}

func SetExecPermissions(ptr unsafe.Pointer, size uintptr) {
	s := unsafe.Slice((*byte)(ptr), int(size))
	err := unix.Mprotect(s, unix.PROT_READ|unix.PROT_WRITE|unix.PROT_EXEC)
	if err != nil {
		panic(err)
	}
}

func VMalloc(size uintptr) unsafe.Pointer {
	if size == 0 {
		return nil
	}
	data, err := unix.Mmap(0, 0, int(pageSize(size)), unix.PROT_READ|unix.PROT_WRITE, unix.MAP_ANON|unix.MAP_PRIVATE)
	if err != nil {
		panic(err)
	}
	return unsafe.Pointer(&data[0])
}

func pageSize(size uintptr) uintptr {
	const page = 4096
	return (size + page - 1) &^ (page - 1)
}
