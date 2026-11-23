package compiler

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"unsafe"
)

type mapBenchRecord struct {
	ID     int64
	Name   string
	Active bool
}

func mapBenchmarkValues() []struct {
	name  string
	value any
} {
	stringsMap := make(map[string]string, 128)
	intsMap := make(map[string]int64, 128)
	intsKeys := make(map[int64]uint64, 128)
	records := make(map[string]mapBenchRecord, 128)
	nested := make(map[string]map[string]int64, 32)
	generic := make(map[string]any, 128)
	for i := 0; i < 128; i++ {
		key := fmt.Sprintf("key_%03d", i)
		stringsMap[key] = "value 中文 " + key
		intsMap[key] = int64(i) * 1234567
		intsKeys[-int64(i)-1] = uint64(i) * 123456789
		records[key] = mapBenchRecord{int64(i), key, i%2 == 0}
		generic[key] = records[key]
		if i < 32 {
			nested[key] = map[string]int64{"a": int64(i), "b": -int64(i), "c": int64(i * 1000)}
		}
	}
	return []struct {
		name  string
		value any
	}{
		{"Nil", map[string]int(nil)}, {"Empty", map[string]int{}}, {"Small", map[string]int{"a": 1, "b": 2, "c": 3}},
		{"StringInt", intsMap}, {"StringString", stringsMap}, {"IntUint", intsKeys}, {"Struct", records}, {"Nested", nested}, {"Interface", generic},
		{"EscapedKeys", map[string]string{"\"\\\n\t<>&\u2028\u2029": strings.Repeat("\x00\n\\\"中文", 16)}},
	}
}

func BenchmarkJITMap(b *testing.B) {
	for _, test := range mapBenchmarkValues() {
		b.Run(test.name, func(b *testing.B) {
			compiled, err := compileJITEncoder(reflect.TypeOf(test.value))
			if err != nil {
				b.Fatal(err)
			}
			data := reflect.ValueOf(test.value).UnsafePointer()
			dst, err := compiled.encode(nil, data)
			if err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				dst, err = compiled.encode(dst, data)
				if err != nil {
					b.Fatal(err)
				}
			}
			b.StopTimer()
			b.ReportMetric(float64(compiled.stats.CodeBytes), "code-B")
		})
	}
}

func BenchmarkJITMapCompile(b *testing.B) {
	for _, test := range mapBenchmarkValues() {
		b.Run(test.name, func(b *testing.B) {
			typ := reflect.TypeOf(test.value)
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				compiled, err := compileJITEncoder(typ)
				if err != nil {
					b.Fatal(err)
				}
				inlineBenchmarkSink = unsafe.Pointer(compiled)
			}
		})
	}
}
