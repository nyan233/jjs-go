package asm

import "unsafe"

//go:linkname RtMemoryCopy runtime.memmove
func RtMemoryCopy(to unsafe.Pointer, from unsafe.Pointer, n uintptr)
