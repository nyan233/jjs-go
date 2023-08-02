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

func VMalloc(size uintptr) unsafe.Pointer {
	data, err := unix.Mmap(0, 0, int(size), unix.PROT_READ|unix.PROT_WRITE|unix.PROT_EXEC, unix.MAP_SHARED)
	if err != nil {
		panic(err)
	}
	return unsafe.Pointer(&data[0])
}
