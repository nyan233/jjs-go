package asm

import "unsafe"

func CPUID() [2]uint64

func JitMemoryCopy()

func JitSave()
func JitRecover()

func CallMFunc(text, stackHi uintptr, dest, data unsafe.Pointer) (write int64, reason int64)
