package sys

import (
	"github.com/nyan233/jjs-go/internal/asm"
	"golang.org/x/sys/unix"
	"reflect"
	"unsafe"
)

func ModifyPermissions2Exec(code []byte) []byte {
	ptr := VMalloc(4096)
	newSpace := *(*[]byte)(unsafe.Pointer(&reflect.SliceHeader{
		Data: uintptr(ptr),
		Len:  len(code),
		Cap:  4096,
	}))
	asm.RtMemoryCopy(ptr, unsafe.Pointer(&code[0]), uintptr(len(code)))
	err := unix.Mprotect(newSpace, unix.PROT_READ|unix.PROT_EXEC)
	if err != nil {
		panic(err)
	}
	return newSpace
}

func SetExecPermissions(ptr unsafe.Pointer, size uintptr) {
	s := *(*[]byte)(unsafe.Pointer(&reflect.SliceHeader{
		Data: uintptr(ptr),
		Len:  int(size),
		Cap:  int(size),
	}))
	err := unix.Mprotect(s, unix.PROT_READ|unix.PROT_WRITE|unix.PROT_EXEC)
	if err != nil {
		panic(err)
	}
}

func VMalloc(size uintptr) unsafe.Pointer {
	data, err := unix.Mmap(0, 0, int(size), unix.PROT_READ|unix.PROT_WRITE, unix.MAP_ANON|unix.MAP_PRIVATE)
	if err != nil {
		panic(err)
	}
	return unsafe.Pointer(&data[0])
}
