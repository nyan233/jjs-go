//go:build amd64

package compiler

import (
	"runtime"
	"unsafe"
)

//go:noescape
func base64EncodeAVX2(dst, src *byte, length int, alphabet *byte)

func encodeBase64(dst, src []byte) {
	for len(src) > base64ChunkBytes {
		base64EncodeAVX2(
			unsafe.SliceData(dst),
			unsafe.SliceData(src),
			base64ChunkBytes,
			unsafe.StringData(base64Alphabet),
		)
		dst = dst[base64ChunkBytes/3*4:]
		src = src[base64ChunkBytes:]
		runtime.Gosched()
	}
	base64EncodeAVX2(unsafe.SliceData(dst), unsafe.SliceData(src), len(src), unsafe.StringData(base64Alphabet))
}
