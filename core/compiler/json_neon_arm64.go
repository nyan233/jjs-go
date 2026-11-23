package compiler

import (
	"math/bits"
	"unsafe"
)

//go:noescape
func scanJSONSpecialNEON(ptr *byte, n int, html uint64) int

func scanJSONEscape(ptr *byte, n int) int {
	if n >= 24 {
		word := *(*uint64)(unsafe.Pointer(ptr))
		mask := jsonByteEqualMask(word, 0x2222222222222222) |
			jsonByteEqualMask(word, 0x5c5c5c5c5c5c5c5c) |
			jsonByteEqualMask(word, 0x3c3c3c3c3c3c3c3c) |
			jsonByteEqualMask(word, 0x3e3e3e3e3e3e3e3e) |
			jsonByteEqualMask(word, 0x2626262626262626) |
			jsonByteLessThan32(word) | word&jsonByteHigh
		if mask != 0 {
			return bits.TrailingZeros64(mask) / 8
		}
		if index := scanJSONSpecialNEON((*byte)(unsafe.Add(unsafe.Pointer(ptr), 8)), n-8, 1); index >= 0 {
			return index + 8
		}
		return -1
	}
	for index := 0; index+8 <= n; index += 8 {
		word := *(*uint64)(unsafe.Add(unsafe.Pointer(ptr), index))
		mask := jsonByteEqualMask(word, 0x2222222222222222) |
			jsonByteEqualMask(word, 0x5c5c5c5c5c5c5c5c) |
			jsonByteEqualMask(word, 0x3c3c3c3c3c3c3c3c) |
			jsonByteEqualMask(word, 0x3e3e3e3e3e3e3e3e) |
			jsonByteEqualMask(word, 0x2626262626262626) |
			jsonByteLessThan32(word) |
			word&jsonByteHigh
		if mask != 0 {
			return index + bits.TrailingZeros64(mask)/8
		}
	}
	return scanJSONEscapeBytes(ptr, n, n&^7)
}

// String scanning receives the remaining document length. Most matches are
// close by, and the full-document benchmark favors word scanning here even
// though NEON wins the long-ASCII microbenchmark.
func scanJSONStringEnd(ptr *byte, n int) int {
	return scanJSONStringEndWord(ptr, n)
}
