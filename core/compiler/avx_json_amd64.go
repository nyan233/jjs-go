//go:build amd64

package compiler

import "golang.org/x/sys/cpu"

func init() {
	if !cpu.X86.HasAVX2 {
		panic("jjs-go requires an AVX2-capable CPU")
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
