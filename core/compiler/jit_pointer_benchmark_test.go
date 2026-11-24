package compiler

import (
	"fmt"
	"reflect"
	"testing"
	"unsafe"
)

type pointerBenchNode struct {
	Value int
	Next  *pointerBenchNode
}

type pointerBenchWrapper struct {
	Before  int
	Pointer *pointerBenchNode
	After   int
}

func pointerBenchmarkValues() []struct {
	name  string
	value any
} {
	var chain *pointerBenchNode
	for i := 0; i < 32; i++ {
		chain = &pointerBenchNode{i, chain}
	}
	fields := make([]reflect.StructField, 64)
	for i := range fields {
		fields[i] = reflect.StructField{Name: fmt.Sprintf("F%02d", i), Type: reflect.TypeOf(int64(0))}
	}
	large := reflect.StructOf(fields)
	wrapper := reflect.StructOf([]reflect.StructField{
		{Name: "Before", Type: reflect.TypeOf(int(0))},
		{Name: "Pointer", Type: reflect.PointerTo(large)},
		{Name: "After", Type: reflect.TypeOf(int(0))},
	})
	nilLarge, valueLarge := reflect.New(wrapper), reflect.New(wrapper)
	object := reflect.New(large)
	for i := range fields {
		object.Elem().Field(i).SetInt(int64(i * 12345))
	}
	valueLarge.Elem().Field(1).Set(object)
	valueLarge.Elem().Field(0).SetInt(7)
	valueLarge.Elem().Field(2).SetInt(9)
	nilLarge.Elem().Field(0).SetInt(7)
	nilLarge.Elem().Field(2).SetInt(9)
	nilSlice := make([]*pointerBenchNode, 128)
	leaves := make([]*pointerBenchNode, 128)
	for i := range leaves {
		leaves[i] = &pointerBenchNode{Value: i}
	}
	number := 42
	return []struct {
		name  string
		value any
	}{
		{"InlineScalar", &struct {
			Pointer *int
			After   int
		}{&number, 9}},
		{"NilRecursive", &pointerBenchWrapper{Before: 7, After: 9}},
		{"Recursive32", &pointerBenchWrapper{7, chain, 9}},
		{"NilLarge", nilLarge.Interface()},
		{"Large", valueLarge.Interface()},
		{"NilSlice128", &nilSlice},
		{"Leaves128", &leaves},
	}
}

func BenchmarkJITPointer(b *testing.B) {
	for _, tc := range pointerBenchmarkValues() {
		b.Run(tc.name, func(b *testing.B) {
			p, err := compileJITEncoder(reflect.TypeOf(tc.value))
			if err != nil {
				b.Fatal(err)
			}
			ptr := reflect.ValueOf(tc.value).UnsafePointer()
			dst, err := p.encode(nil, ptr)
			if err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				dst, err = p.encode(dst, ptr)
				if err != nil {
					b.Fatal(err)
				}
			}
			b.StopTimer()
			b.ReportMetric(float64(p.stats.CodeBytes), "code-B")
		})
	}
}

func BenchmarkJITPointerCompile(b *testing.B) {
	for _, tc := range pointerBenchmarkValues() {
		b.Run(tc.name, func(b *testing.B) {
			typ := reflect.TypeOf(tc.value)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				p, err := compileJITEncoder(typ)
				if err != nil {
					b.Fatal(err)
				}
				inlineBenchmarkSink = unsafe.Pointer(p)
			}
		})
	}
}
