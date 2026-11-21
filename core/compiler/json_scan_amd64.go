//go:build amd64 && !arm64_asm_test

package compiler

const jitEncodeShortScanBytes = 4
const jitDecodeStringVectorBytes = 256

func scanJSONEscape(ptr *byte, n int) int {
	if n >= 256 {
		return avx2FindJSONEscape(ptr, n)
	}
	return sseFindJSONEscape(ptr, n)
}

func scanJSONStringEnd(ptr *byte, n int) int {
	return avx2FindJSONStringEnd(ptr, n)
}
