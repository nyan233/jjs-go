package sys

import (
	"github.com/nyan233/jjs-go/internal/asm"
	"golang.org/x/sys/windows"
	"reflect"
	"unsafe"
)

var (
	memDLL         = windows.NewLazyDLL("kernel32.dll")
	virtualAlloc   = memDLL.NewProc("VirtualAlloc")
	virtualProtect = memDLL.NewProc("VirtualProtect")
)

func ModifyPermissions2Exec(code []byte) []byte {
	addr := VMalloc(4096)
	asm.RtMemoryCopy(addr, unsafe.Pointer(&code[0]), uintptr(len(code)))
	oldPermission := windows.PAGE_EXECUTE_READWRITE
	_, _, err := virtualProtect.Call(uintptr(addr), 4096, windows.PAGE_EXECUTE_READ, uintptr(unsafe.Pointer(&oldPermission)))
	if err != nil && err != windows.DS_S_SUCCESS {
		panic(err)
	}
	return *(*[]byte)(unsafe.Pointer(&reflect.SliceHeader{
		Data: uintptr(addr),
		Len:  len(code),
		Cap:  4096,
	}))
}

func VMalloc(size uintptr) unsafe.Pointer {
	addr, _, err := virtualAlloc.Call(0, size, windows.MEM_COMMIT|windows.MEM_RESERVE, windows.PAGE_EXECUTE_READWRITE)
	if err != nil && err != windows.DS_S_SUCCESS {
		panic(err)
	}
	return unsafe.Pointer(addr)
}
