package iasm

import (
	"github.com/nyan233/jjs-go/internal/rtype"
	"unsafe"
)

var BytesEtype = func() unsafe.Pointer {
	eface := any([]byte{})
	return (*[2]unsafe.Pointer)(unsafe.Pointer(&eface))[0]
}()

//go:linkname growslice runtime.growslice
func growslice(oldPtr unsafe.Pointer, newLen, oldCap, num int, et unsafe.Pointer) rtype.Slice

//go:linkname memmove runtime.memmove
func memmove(to, from unsafe.Pointer, n uintptr)
