package sys

import (
	"github.com/nyan233/jjs-go/internal/rtype"
	"unsafe"
)

// AABytes AllocAlignBytes
func AABytes(size uintptr) []byte {
	const Align = 16
	aCap := size + Align - (size % (Align))
	ptr := VMalloc(aCap)
	header := rtype.NPSlice{
		Ptr: uintptr(ptr),
		Len: int(size),
		Cap: int(aCap),
	}
	return *(*[]byte)(unsafe.Pointer(&header))
}
