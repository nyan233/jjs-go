package compiler

import (
	"fmt"
	"reflect"
	"testing"
)

// Measures code generation without registering executable modules forever.
// Keep the same benchmark in the baseline checkout for before/after results.
func BenchmarkJITCodegen(b *testing.B) {
	for _, n := range []int{4, 32} {
		b.Run(fmt.Sprintf("Fields%d", n), func(b *testing.B) {
			fields := make([]reflect.StructField, n)
			for i := range fields {
				fields[i] = reflect.StructField{Name: fmt.Sprintf("Field%d", i), Type: reflect.TypeOf(int64(0))}
			}
			typ := reflect.StructOf(fields)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				compiler := &jitEncoderCompiler{unitByType: make(map[jitEncoderUnitKey]int)}
				if _, err := compiler.unitIndex(typ, false); err != nil {
					b.Fatal(err)
				}
				g := &jitEncoderGenerator{code: compiler.units[0].code, compiler: compiler}
				if err := g.emitPrologue(); err != nil {
					b.Fatal(err)
				}
				if err := g.emitValue(typ, 0); err != nil {
					b.Fatal(err)
				}
				g.emitReturn()
				if _, err := g.code.assemble(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
