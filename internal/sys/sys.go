package sys

import (
	"unsafe"
)

// AABytes AllocAlignBytes
func AABytes(size uintptr) []byte {
	const Align = 16
	aCap := size + Align - (size % (Align))
	ptr := VMalloc(aCap)
	return unsafe.Slice((*byte)(ptr), int(aCap))[:int(size)]
}
