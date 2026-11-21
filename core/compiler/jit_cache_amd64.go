//go:build amd64 && !arm64_asm_test

package compiler

func flushJITCode(_ uintptr, _ int) {}
