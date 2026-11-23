package compare

import (
	"reflect"
	"testing"

	"github.com/nyan233/jjs-go/core/compiler"
)

// Use fixed iterations: each call registers an executable module.
func BenchmarkSSACompileFixture(b *testing.B) {
	for _, tc := range []struct {
		name string
		typ  reflect.Type
	}{
		{"Book", reflect.TypeOf(Book{})},
		{"Twitter", reflect.TypeOf(TwitterResponse{})},
	} {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				stats, err := compiler.EncoderCodegenStats(tc.typ)
				if err != nil {
					b.Fatal(err)
				}
				b.ReportMetric(float64(stats.CodeBytes), "code-B")
				b.ReportMetric(float64(stats.InstructionsAfter), "instructions")
			}
		})
	}
}
