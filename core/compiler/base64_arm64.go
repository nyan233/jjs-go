package compiler

import (
	"runtime"
	"unsafe"
)

//go:generate go run ../../internal/cmd/genbase64

//go:noescape
func base64EncodeNEON(dst, src *byte, length int, alphabet *byte)

func encodeBase64(dst, src []byte) {
	for len(src) > base64ChunkBytes {
		base64EncodeNEON(unsafe.SliceData(dst), unsafe.SliceData(src), base64ChunkBytes, unsafe.StringData(base64Alphabet))
		dst = dst[base64ChunkBytes/3*4:]
		src = src[base64ChunkBytes:]
		runtime.Gosched()
	}
	base64EncodeNEON(unsafe.SliceData(dst), unsafe.SliceData(src), len(src), unsafe.StringData(base64Alphabet))
}
