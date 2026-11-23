//go:build !arm64 && arm64_asm_test

package compiler

func scanJSONEscape(ptr *byte, n int) int { return scanJSONEscapeWord(ptr, n) }

func scanJSONStringEnd(ptr *byte, n int) int { return scanJSONStringEndWord(ptr, n) }
