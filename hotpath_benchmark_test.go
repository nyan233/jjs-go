package jjs_go

import (
	"encoding/json"
	"fmt"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"unsafe"

	"github.com/nyan233/jjs-go/core/compiler"
)

var hotpathBytes []byte

func BenchmarkHotpathStrings(b *testing.B) {
	for _, test := range []struct{ name, value string }{
		{"ASCII", strings.Repeat("abcdefgh", 128)},
		{"UnicodeThenASCII", "中文" + strings.Repeat("abcdefgh", 128)},
		{"Unicode", strings.Repeat("中文测试", 64)},
		{"LateEscape", strings.Repeat("abcdefgh", 128) + "\n"},
		{"Escapes", strings.Repeat("a\n\"\\中文", 32)},
	} {
		b.Run(test.name, func(b *testing.B) {
			encoded, err := Marshal(test.value)
			if err != nil {
				b.Fatal(err)
			}
			b.Run("Encode", func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					hotpathBytes, err = Marshal(test.value)
					if err != nil {
						b.Fatal(err)
					}
				}
			})
			b.Run("Decode", func(b *testing.B) {
				var value string
				if err := Unmarshal(encoded, &value); err != nil {
					b.Fatal(err)
				}
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if err := Unmarshal(encoded, &value); err != nil {
						b.Fatal(err)
					}
				}
				runtime.KeepAlive(value)
			})
		})
	}
}

type hotpathRecord struct {
	ID     int    `json:"id"`
	Count  int    `json:"count"`
	Name   string `json:"name"`
	Active bool   `json:"active"`
	Note   string `json:"note,omitempty"`
}

func BenchmarkHotpathDynamicFields(b *testing.B) {
	value := hotpathRecord{ID: 42, Count: 7, Name: "record", Active: true, Note: "note"}
	if _, err := Marshal(value); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var err error
		hotpathBytes, err = Marshal(value)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkHotpathInterfaces(b *testing.B) {
	value := []any{"text", 42, true, 3.5, hotpathRecord{ID: 7}, map[string]any{"key": "value"}}
	if _, err := Marshal(value); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var err error
		hotpathBytes, err = Marshal(value)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkHotpathWideDecode(b *testing.B) {
	fields := make([]reflect.StructField, 32)
	input := make(map[string]int, len(fields))
	for i := range fields {
		name := fmt.Sprintf("Field%02d", i)
		fields[i] = reflect.StructField{Name: name, Type: reflect.TypeOf(int(0))}
		input[name] = i * 1234567
	}
	target := reflect.New(reflect.StructOf(fields)).Interface()
	data, err := json.Marshal(input)
	if err != nil {
		b.Fatal(err)
	}
	if err := Unmarshal(data, target); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := Unmarshal(data, target); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkHotpathIntegers(b *testing.B) {
	data := []byte(`[0,1,-1,127,-128,32767,-32768,2147483647,-2147483648,9223372036854775807,-9223372036854775808]`)
	var target []int64
	if err := Unmarshal(data, &target); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := Unmarshal(data, &target); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkHotpathBufferReuse(b *testing.B) {
	value := hotpathRecord{ID: 42, Name: "record", Note: "reuse"}
	handler, err := compiler.CompileEncoder(reflect.TypeOf(value), 0)
	if err != nil {
		b.Fatal(err)
	}
	var dst []byte
	if err := handler(&dst, unsafe.Pointer(&value)); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := handler(&dst, unsafe.Pointer(&value)); err != nil {
			b.Fatal(err)
		}
	}
	hotpathBytes = dst
}

func BenchmarkHotpathParallelCache(b *testing.B) {
	typ := reflect.TypeOf(hotpathRecord{})
	if _, err := compiler.CompileEncoder(typ, 0); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if _, err := compiler.CompileEncoder(typ, 0); err != nil {
				b.Error(err)
				return
			}
		}
	})
}

func BenchmarkHotpathMixedSizes(b *testing.B) {
	small := &hotpathRecord{ID: 42, Name: "small"}
	large := &hotpathRecord{ID: 42, Name: strings.Repeat("large", 400000)}
	if _, err := Marshal(small); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		value := small
		if i%32 == 0 {
			value = large
		}
		var err error
		hotpathBytes, err = Marshal(value)
		if err != nil {
			b.Fatal(err)
		}
	}
}
