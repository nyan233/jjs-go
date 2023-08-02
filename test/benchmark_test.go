package test

import (
	"math"
	"os"
	"testing"
	"unsafe"
)

func BenchmarkMemoryCopy(b *testing.B) {
	const SIZE = 1024 * 256
	var d1 = make([]byte, SIZE, SIZE)
	var d2 = make([]byte, SIZE, SIZE)
	d := os.Getpid()
	b.Log(d)
	b.Run("GO-Native", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			copy(d1, d2)
		}
	})
	b.Run("GO-Normal", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			movMemoryCopy(&d1, &d2, SIZE)
		}
	})
	b.Run("GO-AVX-256", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			callJitMemCopy(unsafe.Pointer(&d1[0]), unsafe.Pointer(&d2[0]), SIZE)
		}
	})
}

func movMemoryCopy(to, from *[]byte, n uintptr) {
	for i := n - 1; i != math.MaxUint64; i-- {
		(*to)[i] = (*from)[i]
	}
}
