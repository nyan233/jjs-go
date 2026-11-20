package sys

import (
	"github.com/nyan233/jjs-go/internal/asm"
	"golang.org/x/sys/windows"
	"unsafe"
)

var (
	memDLL         = windows.NewLazyDLL("kernel32.dll")
	virtualAlloc   = memDLL.NewProc("VirtualAlloc")
	virtualProtect = memDLL.NewProc("VirtualProtect")
)

func ModifyPermissions2Exec(code []byte) []byte {
	if len(code) == 0 {
		return []byte{}
	}
	size := pageSize(uintptr(len(code)))
	addr := VMalloc(size)
	asm.RtMemoryCopy(addr, unsafe.Pointer(&code[0]), uintptr(len(code)))
	var oldPermission uint32
	ok, _, err := virtualProtect.Call(uintptr(addr), size, windows.PAGE_EXECUTE_READ, uintptr(unsafe.Pointer(&oldPermission)))
	if ok == 0 {
		panic(err)
	}
	return unsafe.Slice((*byte)(addr), int(size))[:len(code)]
}

// SetExecPermissions marks a virtual-memory allocation as executable. It is
// intentionally separate from page protection so ordinary heap allocation can
// remain non-executable on Windows.
func SetExecPermissions(ptr unsafe.Pointer, size uintptr) {
	if ptr == nil || size == 0 {
		return
	}
	var oldPermission uint32
	ok, _, err := virtualProtect.Call(
		uintptr(ptr),
		pageSize(size),
		windows.PAGE_EXECUTE_READWRITE,
		uintptr(unsafe.Pointer(&oldPermission)),
	)
	if ok == 0 {
		panic(err)
	}
}

func VMalloc(size uintptr) unsafe.Pointer {
	if size == 0 {
		return nil
	}
	addr, _, err := virtualAlloc.Call(0, pageSize(size), windows.MEM_COMMIT|windows.MEM_RESERVE, windows.PAGE_READWRITE)
	if addr == 0 {
		panic(err)
	}
	return unsafe.Pointer(addr)
}

func pageSize(size uintptr) uintptr {
	const page = 4096
	return (size + page - 1) &^ (page - 1)
}
