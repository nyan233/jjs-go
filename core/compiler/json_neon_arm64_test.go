package compiler

import (
	"bytes"
	"fmt"
	"testing"
	"unsafe"
)

func TestARM64NEONScanner(t *testing.T) {
	for _, n := range []int{0, 1, 7, 8, 15, 16, 17, 23, 31, 32, 33, 47, 63, 64, 65, 127, 128, 129, 255, 256, 257} {
		for offset := 0; offset < 16; offset++ {
			storage := bytes.Repeat([]byte{'a'}, n+offset)
			data := storage[offset:]
			for mode := uint64(0); mode < 2; mode++ {
				if got := scanJSONSpecialNEON(unsafe.SliceData(data), n, mode); got != -1 {
					t.Fatalf("length %d offset %d: got %d", n, offset, got)
				}
				for pos := 0; pos < n; pos++ {
					for _, value := range []byte{0, 31, 32, 33, 34, 35, 38, 60, 62, 92, 127, 128, 255} {
						data[pos] = value
						want := scanJSONStringEndWord(unsafe.SliceData(data), n)
						if mode == 1 {
							want = scanJSONEscapeWord(unsafe.SliceData(data), n)
						}
						if got := scanJSONSpecialNEON(unsafe.SliceData(data), n, mode); got != want {
							t.Fatalf("length %d offset %d position %d byte %x mode %d: got %d want %d", n, offset, pos, value, mode, got, want)
						}
						data[pos] = 'a'
					}
				}
			}
		}
	}
}

var scanResult int

func BenchmarkARM64Scanners(b *testing.B) {
	for _, n := range []int{8, 16, 24, 32, 64, 129, 256, 1024, 4096} {
		for _, shape := range []string{"ASCII", "LateEscape", "EarlyEscape", "Unicode"} {
			data := bytes.Repeat([]byte{'a'}, n)
			switch shape {
			case "LateEscape":
				data[n-1] = '\n'
			case "EarlyEscape":
				data[0] = '\n'
			case "Unicode":
				data[0] = 0xe4
			}
			for _, mode := range []string{"Encode", "Decode"} {
				word, scalar := scanJSONEscapeWord, func(p *byte, n int) int { return scanJSONEscapeBytes(p, n, 0) }
				dispatch := scanJSONEscape
				html := uint64(1)
				if mode == "Decode" {
					word, scalar = scanJSONStringEndWord, func(p *byte, n int) int { return scanJSONStringEndBytes(p, n, 0) }
					dispatch = scanJSONStringEnd
					html = 0
				}
				for _, impl := range []struct {
					name string
					scan func(*byte, int) int
				}{
					{"Byte", scalar}, {"Word", word}, {"NEON", func(p *byte, n int) int { return scanJSONSpecialNEON(p, n, html) }}, {"Dispatch", dispatch},
				} {
					b.Run(fmt.Sprintf("%s/%s/%d/%s", mode, shape, n, impl.name), func(b *testing.B) {
						b.ReportAllocs()
						for i := 0; i < b.N; i++ {
							scanResult = impl.scan(unsafe.SliceData(data), len(data))
						}
					})
				}
			}
		}
	}
}
