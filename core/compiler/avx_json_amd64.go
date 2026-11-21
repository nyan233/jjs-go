//go:build amd64

package compiler

import "golang.org/x/sys/cpu"

func init() {
	if !cpu.X86.HasAVX2 || !cpu.X86.HasSSE42 {
		panic("jjs-go requires an AVX2 and SSE4.2 capable CPU")
	}
}

// avx2FindJSONEscape returns the first byte that needs scalar handling.
// It handles bytes that must be escaped by encoding/json and any non-ASCII
// byte, which is delegated to the existing UTF-8 logic.
//
//go:noescape
func avx2FindJSONEscape(ptr *byte, n int) int

// avx2FindJSONStringEnd returns the first byte that requires scalar decoding:
// a quote, a backslash, a control byte, or a non-ASCII byte.
//
//go:noescape
func avx2FindJSONStringEnd(ptr *byte, n int) int

// sseFindJSONEscape scans strings shorter than the AVX2 path using SSE4.2
// range comparisons, with 8-byte, 4-byte, and scalar tails.
//
//go:noescape
func sseFindJSONEscape(ptr *byte, n int) int
