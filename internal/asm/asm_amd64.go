package asm

import (
	"github.com/nyan233/jjs-go/internal/rtype"
	"unsafe"
)

func CPUID() [2]uint64

func JitMemoryCopy()

func JitSave()
func JitRecover()

func CallMFunc(text, stackHi uintptr, dest, data unsafe.Pointer) (write int64, reason int64)

//go:linkname RTMemmove runtime.memmove
func RTMemmove(to, from unsafe.Pointer, n uintptr)

//go:linkname RTGrowSlice runtime.growslice
func RTGrowSlice(oldPtr unsafe.Pointer, newLen, oldCap, num int, et unsafe.Pointer) rtype.Slice

//go:linkname MakeSlice64 runtime.makeslice64
func MakeSlice64(et uintptr, len64, cap64 int64) unsafe.Pointer

func Append(b *[]byte, s1 string) {
	*b = append(*b, s1...)
}
