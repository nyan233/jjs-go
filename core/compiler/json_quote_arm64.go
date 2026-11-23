package compiler

import (
	"slices"
	"unsafe"
)

const quoteChunkBytes = 64 << 10

// quoteJSONString copies and escapes complete input units into a bounded output
// region, without quotes. It stops before a unit that does not fit, or after
// roughly 64 KiB of input so large strings return to preemptible Go code. Source and
// destination must not overlap. It never reads/writes outside either region.
//
//go:noescape
func quoteJSONString(dst *byte, capacity int, src *byte, length int) (consumed, written int)

func appendQuoted(dst []byte, value string) []byte {
	if len(value) == 0 {
		return append(dst, '"', '"')
	}
	dst = slices.Grow(dst, len(value)+2)
	dst = append(dst, '"')
	for len(value) != 0 {
		start := len(dst)
		output := dst[start : cap(dst)-1] // Reserve the closing quote.
		consumed, written := quoteJSONString(unsafe.SliceData(output), len(output), unsafe.StringData(value), len(value))
		dst = dst[:start+written]
		value = value[consumed:]
		if len(value) != 0 && consumed < quoteChunkBytes {
			// A budget stop needs no growth; an output-capacity stop does.
			// A single escaped byte can need six bytes. Grow on demand instead
			// of reserving six times the input size for normal JSON strings.
			dst = slices.Grow(dst, max(len(value)+1, 7))
		}
	}
	return append(dst, '"')
}
