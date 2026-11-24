//go:build amd64 && !arm64_asm_test

package compiler

import (
	"reflect"
	"slices"
	"unsafe"
)

const quoteChunkBytes = 64 << 10

// quoteJSONString copies and escapes complete input units into a bounded output
// region, without quotes. It stops before a unit that does not fit, or after
// roughly 64 KiB of input so large strings return to preemptible Go code.
// Source and destination must not overlap.
//
//go:noescape
func quoteJSONString(dst *byte, capacity int, src *byte, length int) (consumed, written int)

func appendQuoted(dst []byte, value string) []byte {
	if len(value) == 0 {
		return append(dst, '"', '"')
	}
	dst = slices.Grow(dst, len(value)+2)
	dst = append(dst, '"')
	return appendQuotedTail(dst, value)
}

func appendQuotedTail(dst []byte, value string) []byte {
	for len(value) != 0 {
		start := len(dst)
		output := dst[start : cap(dst)-1]
		consumed, written := quoteJSONString(
			unsafe.SliceData(output),
			len(output),
			unsafe.StringData(value),
			len(value),
		)
		dst = dst[:start+written]
		value = value[consumed:]
		if len(value) != 0 && consumed < quoteChunkBytes {
			dst = slices.Grow(dst, max(len(value)+1, 7))
		}
	}
	return append(dst, '"')
}

//go:noinline
func jitQuoteTail(ctx *jitEncodeContext, ptr unsafe.Pointer, length int) {
	ctx.dst = appendQuotedTail(ctx.dst, unsafe.String((*byte)(ptr), length))
}

func initJITQuoteHelpers(program *jitEncodeProgram) {
	program.helpers[jitHelperQuote] = reflect.ValueOf(quoteJSONString).Pointer()
	program.helpers[jitHelperQuoteTail] = reflect.ValueOf(jitQuoteTail).Pointer()
}
