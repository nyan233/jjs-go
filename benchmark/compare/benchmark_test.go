package compare

import (
	stdjson "encoding/json"
	"reflect"
	"runtime"
	"testing"

	"github.com/bytedance/sonic"
	goccyjson "github.com/goccy/go-json"
	jsoniter "github.com/json-iterator/go"
	"github.com/mailru/easyjson"
	jjs "github.com/nyan233/jjs-go"
)

var (
	sonicStd       = sonic.ConfigStd
	jsoniterStd    = jsoniter.ConfigCompatibleWithStandardLibrary
	benchmarkBytes []byte
	benchmarkAny   any
)

type bindingCodec[T any] struct {
	name      string
	marshal   func(T) ([]byte, error)
	unmarshal func([]byte, *T) error
}

type genericCodec struct {
	name      string
	marshal   func(any) ([]byte, error)
	unmarshal func([]byte, any) error
}

func commonBindingCodecs[T any]() []bindingCodec[T] {
	return []bindingCodec[T]{
		{
			name:      "stdlib",
			marshal:   func(value T) ([]byte, error) { return stdjson.Marshal(value) },
			unmarshal: func(data []byte, target *T) error { return stdjson.Unmarshal(data, target) },
		},
		{
			name:      "goccy",
			marshal:   func(value T) ([]byte, error) { return goccyjson.Marshal(value) },
			unmarshal: func(data []byte, target *T) error { return goccyjson.Unmarshal(data, target) },
		},
		{
			name:      "jsoniter",
			marshal:   func(value T) ([]byte, error) { return jsoniterStd.Marshal(value) },
			unmarshal: func(data []byte, target *T) error { return jsoniterStd.Unmarshal(data, target) },
		},
		{
			name:      "sonic",
			marshal:   func(value T) ([]byte, error) { return sonicStd.Marshal(value) },
			unmarshal: func(data []byte, target *T) error { return sonicStd.Unmarshal(data, target) },
		},
		{
			name:      "jjs-go",
			marshal:   func(value T) ([]byte, error) { return jjs.Marshal(value) },
			unmarshal: func(data []byte, target *T) error { return jjs.Unmarshal(data, target) },
		},
	}
}

func bookCodecs() []bindingCodec[Book] {
	codecs := commonBindingCodecs[Book]()
	return append(codecs, bindingCodec[Book]{
		name: "easyjson",
		marshal: func(value Book) ([]byte, error) {
			return easyjson.Marshal(&value)
		},
		unmarshal: func(data []byte, target *Book) error {
			return easyjson.Unmarshal(data, target)
		},
	})
}

func twitterCodecs() []bindingCodec[TwitterResponse] {
	codecs := commonBindingCodecs[TwitterResponse]()
	return append(codecs, bindingCodec[TwitterResponse]{
		name: "easyjson",
		marshal: func(value TwitterResponse) ([]byte, error) {
			return easyjson.Marshal(&value)
		},
		unmarshal: func(data []byte, target *TwitterResponse) error {
			return easyjson.Unmarshal(data, target)
		},
	})
}

func genericCodecs() []genericCodec {
	return []genericCodec{
		{name: "stdlib", marshal: stdjson.Marshal, unmarshal: stdjson.Unmarshal},
		{name: "goccy", marshal: goccyjson.Marshal, unmarshal: goccyjson.Unmarshal},
		{name: "jsoniter", marshal: jsoniterStd.Marshal, unmarshal: jsoniterStd.Unmarshal},
		{name: "sonic", marshal: sonicStd.Marshal, unmarshal: sonicStd.Unmarshal},
		{name: "jjs-go", marshal: jjs.Marshal, unmarshal: jjs.Unmarshal},
	}
}

func prepareBindingEncode[T any](b *testing.B, codec bindingCodec[T], value T) {
	b.Helper()
	pretouchSonic(b, value)
	if _, err := codec.marshal(value); err != nil {
		b.Fatalf("%s warmup marshal: %v", codec.name, err)
	}
}

func prepareBindingDecode[T any](b *testing.B, codec bindingCodec[T], data []byte) {
	b.Helper()
	var value T
	pretouchSonic(b, value)
	if err := codec.unmarshal(data, &value); err != nil {
		b.Fatalf("%s warmup unmarshal: %v", codec.name, err)
	}
}

func prepareGenericEncode(b *testing.B, codec genericCodec, value any) {
	b.Helper()
	pretouchSonic(b, value)
	if _, err := codec.marshal(value); err != nil {
		b.Fatalf("%s warmup marshal: %v", codec.name, err)
	}
}

func prepareGenericDecode(b *testing.B, codec genericCodec, data []byte) {
	b.Helper()
	pretouchSonic(b, any(map[string]any{}))
	var value any
	if err := codec.unmarshal(data, &value); err != nil {
		b.Fatalf("%s warmup unmarshal: %v", codec.name, err)
	}
}

func pretouchSonic(b *testing.B, value any) {
	b.Helper()
	if err := sonic.Pretouch(reflect.TypeOf(value)); err != nil {
		b.Fatalf("sonic pretouch %T: %v", value, err)
	}
}

func benchmarkBindingEncode[T any](b *testing.B, codecs []bindingCodec[T], value T, payloadSize int) {
	for _, codec := range codecs {
		codec := codec
		b.Run(codec.name, func(b *testing.B) {
			prepareBindingEncode(b, codec, value)
			b.ReportAllocs()
			b.SetBytes(int64(payloadSize))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				data, err := codec.marshal(value)
				if err != nil {
					b.Fatalf("%s marshal: %v", codec.name, err)
				}
				benchmarkBytes = data
			}
			runtime.KeepAlive(value)
		})
	}
}

func benchmarkBindingDecode[T any](b *testing.B, codecs []bindingCodec[T], data []byte) {
	for _, codec := range codecs {
		codec := codec
		b.Run(codec.name, func(b *testing.B) {
			prepareBindingDecode(b, codec, data)
			var target T
			b.ReportAllocs()
			b.SetBytes(int64(len(data)))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := codec.unmarshal(data, &target); err != nil {
					b.Fatalf("%s unmarshal: %v", codec.name, err)
				}
			}
			benchmarkAny = target
		})
	}
}

func benchmarkGenericEncode(b *testing.B, value any, payloadSize int) {
	for _, codec := range genericCodecs() {
		codec := codec
		b.Run(codec.name, func(b *testing.B) {
			prepareGenericEncode(b, codec, value)
			b.ReportAllocs()
			b.SetBytes(int64(payloadSize))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				data, err := codec.marshal(value)
				if err != nil {
					b.Fatalf("%s marshal: %v", codec.name, err)
				}
				benchmarkBytes = data
			}
			runtime.KeepAlive(value)
		})
	}
}

func benchmarkGenericDecode(b *testing.B, data []byte) {
	for _, codec := range genericCodecs() {
		codec := codec
		b.Run(codec.name, func(b *testing.B) {
			prepareGenericDecode(b, codec, data)
			var target any
			b.ReportAllocs()
			b.SetBytes(int64(len(data)))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				target = nil
				if err := codec.unmarshal(data, &target); err != nil {
					b.Fatalf("%s unmarshal: %v", codec.name, err)
				}
			}
			benchmarkAny = target
		})
	}
}

func BenchmarkEncoder_Binding(b *testing.B) {
	b.Run("Small", func(b *testing.B) {
		benchmarkBindingEncode(b, bookCodecs(), SmallBook, len(SmallJSON))
	})
	b.Run("Medium", func(b *testing.B) {
		benchmarkBindingEncode(b, twitterCodecs(), MediumValue, len(MediumJSON))
	})
	b.Run("Large", func(b *testing.B) {
		benchmarkBindingEncode(b, twitterCodecs(), LargeValue, len(LargeJSON))
	})
}

func BenchmarkDecoder_Binding(b *testing.B) {
	b.Run("Small", func(b *testing.B) {
		benchmarkBindingDecode(b, bookCodecs(), SmallJSON)
	})
	b.Run("Medium", func(b *testing.B) {
		benchmarkBindingDecode(b, twitterCodecs(), MediumJSON)
	})
	b.Run("Large", func(b *testing.B) {
		benchmarkBindingDecode(b, twitterCodecs(), LargeJSON)
	})
}

func BenchmarkEncoder_Generic(b *testing.B) {
	b.Run("Small", func(b *testing.B) {
		benchmarkGenericEncode(b, SmallGeneric, len(SmallJSON))
	})
	b.Run("Medium", func(b *testing.B) {
		benchmarkGenericEncode(b, MediumGeneric, len(MediumJSON))
	})
	b.Run("Large", func(b *testing.B) {
		benchmarkGenericEncode(b, LargeGeneric, len(LargeJSON))
	})
}

func BenchmarkDecoder_Generic(b *testing.B) {
	b.Run("Small", func(b *testing.B) {
		benchmarkGenericDecode(b, SmallJSON)
	})
	b.Run("Medium", func(b *testing.B) {
		benchmarkGenericDecode(b, MediumJSON)
	})
	b.Run("Large", func(b *testing.B) {
		benchmarkGenericDecode(b, LargeJSON)
	})
}
