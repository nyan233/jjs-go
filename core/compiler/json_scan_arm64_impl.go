//go:build arm64 || arm64_asm_test

package compiler

import (
	"math/bits"
	"unsafe"
)

const jitEncodeShortScanBytes = 8
const jitDecodeStringVectorBytes = 8

const (
	jsonByteOnes uint64 = 0x0101010101010101
	jsonByteHigh uint64 = 0x8080808080808080
)

func scanJSONEscapeWord(ptr *byte, n int) int {
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

func scanJSONStringEndWord(ptr *byte, n int) int {
	for index := 0; index+8 <= n; index += 8 {
		word := *(*uint64)(unsafe.Add(unsafe.Pointer(ptr), index))
		mask := jsonByteEqualMask(word, 0x2222222222222222) |
			jsonByteEqualMask(word, 0x5c5c5c5c5c5c5c5c) |
			jsonByteLessThan32(word) |
			word&jsonByteHigh
		if mask != 0 {
			return index + bits.TrailingZeros64(mask)/8
		}
	}
	return scanJSONStringEndBytes(ptr, n, n&^7)
}

func scanJSONEscapeBytes(ptr *byte, n, start int) int {
	for index := start; index < n; index++ {
		value := *(*byte)(unsafe.Add(unsafe.Pointer(ptr), index))
		if value < 0x20 || value >= 0x80 ||
			value == '"' || value == '\\' || value == '<' || value == '>' || value == '&' {
			return index
		}
	}
	return -1
}

func scanJSONStringEndBytes(ptr *byte, n, start int) int {
	for index := start; index < n; index++ {
		value := *(*byte)(unsafe.Add(unsafe.Pointer(ptr), index))
		if value < 0x20 || value >= 0x80 || value == '"' || value == '\\' {
			return index
		}
	}
	return -1
}

func jsonByteEqualMask(word, repeated uint64) uint64 {
	value := word ^ repeated
	return (value - jsonByteOnes) &^ value & jsonByteHigh
}

func jsonByteLessThan32(word uint64) uint64 {
	return (word - 0x2020202020202020) &^ word & jsonByteHigh
}
