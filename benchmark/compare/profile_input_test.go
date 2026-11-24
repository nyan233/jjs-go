package compare

import (
	"reflect"
	"testing"
)

// This diagnostic isolates Sonic's byte-to-string input copy. It uses the
// same decoder options and fixtures as the primary byte-input comparison;
// its String arm deliberately prepares the immutable input outside timing.
func benchmarkSonicInputCopy[T any](b *testing.B, data []byte, want T) {
	input := string(data)
	for _, name := range []string{"Bytes", "String"} {
		b.Run(name, func(b *testing.B) {
			var target T
			decode := func() error { return sonicStd.Unmarshal(data, &target) }
			if name == "String" {
				decode = func() error { return sonicStd.UnmarshalFromString(input, &target) }
			}
			if err := decode(); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.SetBytes(int64(len(data)))
			b.ResetTimer()
			for range b.N {
				if err := decode(); err != nil {
					b.Fatal(err)
				}
			}
			b.StopTimer()
			if !reflect.DeepEqual(target, want) {
				b.Fatal("decoded value differs from the fixture")
			}
		})
	}
}

func BenchmarkSonicInputCopy(b *testing.B) {
	b.Run("Small", func(b *testing.B) { benchmarkSonicInputCopy(b, SmallJSON, SmallBook) })
	b.Run("Medium", func(b *testing.B) { benchmarkSonicInputCopy(b, MediumJSON, MediumValue) })
	b.Run("Large", func(b *testing.B) { benchmarkSonicInputCopy(b, LargeJSON, LargeValue) })
}
