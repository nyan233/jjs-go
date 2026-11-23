package compiler

import (
	"fmt"
	"reflect"
	"testing"
	"unsafe"
)

type inlineBenchRecord struct {
	ID     int64
	Count  uint32
	Active bool
	Ratio  float64
}

func inlineBenchmarkValues() []struct {
	name  string
	value any
} {
	integer := int64(-1234567890123456789)
	record := inlineBenchRecord{42, 12345, true, 3.141592653589793}
	values := []struct {
		name  string
		value any
	}{
		{"Bool", new(bool)},
		{"IntSmall", new(int64)},
		{"IntLarge", &integer},
		{"Record", &record},
		{"Pointer", &struct{ Value *inlineBenchRecord }{&record}},
		{"Ints", &[]int64{0, 1, -1, 127, -128, 32767, -32768, 2147483647, -2147483648, 9223372036854775807, -9223372036854775808}},
	}
	records := make([]inlineBenchRecord, 128)
	for i := range records {
		records[i] = record
	}
	values = append(values, struct {
		name  string
		value any
	}{"Records", &records})
	for _, n := range []int{0, 12, 48, 1024, 65536} {
		data := make([]byte, n)
		for i := range data {
			data[i] = byte(i)
		}
		values = append(values, struct {
			name  string
			value any
		}{fmt.Sprintf("Bytes%d", n), &data})
	}
	return values
}

func BenchmarkJITInline(b *testing.B) {
	for _, value := range inlineBenchmarkValues() {
		b.Run(value.name, func(b *testing.B) {
			program, err := compileJITEncoder(reflect.TypeOf(value.value))
			if err != nil {
				b.Fatal(err)
			}
			ptr := reflect.ValueOf(value.value).UnsafePointer()
			dst, err := program.encode(nil, ptr)
			if err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				dst, err = program.encode(dst, ptr)
				if err != nil {
					b.Fatal(err)
				}
			}
			b.StopTimer()
			b.ReportMetric(float64(program.stats.CodeBytes), "code-B")
		})
	}
}

// Run with a fixed iteration count: generated code is retained by the runtime.
func BenchmarkJITInlineCompile(b *testing.B) {
	for _, value := range inlineBenchmarkValues()[:7] {
		b.Run(value.name, func(b *testing.B) {
			typ := reflect.TypeOf(value.value)
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				program, err := compileJITEncoder(typ)
				if err != nil {
					b.Fatal(err)
				}
				inlineBenchmarkSink = unsafe.Pointer(program)
			}
		})
	}
}

var inlineBenchmarkSink unsafe.Pointer
