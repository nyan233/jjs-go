package compare

import (
	stdjson "encoding/json"
	"reflect"
	"testing"
)

func TestFixtureSizes(t *testing.T) {
	tests := []struct {
		name       string
		data       []byte
		minLength  int
		maxLength  int
		exactBytes int
	}{
		{name: "Small", data: SmallJSON, minLength: 300, maxLength: 500},
		{name: "Medium", data: MediumJSON, minLength: 13_000, maxLength: 14_000},
		{name: "Large", data: LargeJSON, exactBytes: 646_995},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.exactBytes != 0 {
				if len(test.data) != test.exactBytes {
					t.Fatalf("fixture size = %d, want %d", len(test.data), test.exactBytes)
				}
				return
			}
			if len(test.data) < test.minLength || len(test.data) > test.maxLength {
				t.Fatalf("fixture size = %d, want range [%d, %d]", len(test.data), test.minLength, test.maxLength)
			}
		})
	}
}

func TestBindingCodecsRoundTrip(t *testing.T) {
	t.Run("Small", func(t *testing.T) {
		testBindingRoundTrip(t, SmallJSON, SmallBook, bookCodecs())
	})
	t.Run("Medium", func(t *testing.T) {
		testBindingRoundTrip(t, MediumJSON, MediumValue, twitterCodecs())
	})
	t.Run("Large", func(t *testing.T) {
		testBindingRoundTrip(t, LargeJSON, LargeValue, twitterCodecs())
	})
}

func testBindingRoundTrip[T any](t *testing.T, data []byte, want T, codecs []bindingCodec[T]) {
	t.Helper()
	for _, codec := range codecs {
		codec := codec
		t.Run(codec.name, func(t *testing.T) {
			var decoded T
			if err := codec.unmarshal(data, &decoded); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if !reflect.DeepEqual(decoded, want) {
				t.Fatalf("decoded value differs from encoding/json")
			}

			encoded, err := codec.marshal(want)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			var roundTrip T
			if err := stdjson.Unmarshal(encoded, &roundTrip); err != nil {
				t.Fatalf("encoding/json rejected output: %v", err)
			}
			if !reflect.DeepEqual(roundTrip, want) {
				t.Fatalf("round-trip value differs from encoding/json")
			}
		})
	}
}

func TestGenericCodecsRoundTrip(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want any
	}{
		{name: "Small", data: SmallJSON, want: SmallGeneric},
		{name: "Medium", data: MediumJSON, want: MediumGeneric},
		{name: "Large", data: LargeJSON, want: LargeGeneric},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			for _, codec := range genericCodecs() {
				codec := codec
				t.Run(codec.name, func(t *testing.T) {
					var decoded any
					if err := codec.unmarshal(test.data, &decoded); err != nil {
						t.Fatalf("unmarshal: %v", err)
					}
					if !reflect.DeepEqual(decoded, test.want) {
						t.Fatalf("decoded value differs from encoding/json")
					}

					encoded, err := codec.marshal(test.want)
					if err != nil {
						t.Fatalf("marshal: %v", err)
					}
					var roundTrip any
					if err := stdjson.Unmarshal(encoded, &roundTrip); err != nil {
						t.Fatalf("encoding/json rejected output: %v", err)
					}
					if !reflect.DeepEqual(roundTrip, test.want) {
						t.Fatalf("round-trip value differs from encoding/json")
					}
				})
			}
		})
	}
}
