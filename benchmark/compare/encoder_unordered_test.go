package compare

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/bytedance/sonic"
	goccyjson "github.com/goccy/go-json"
)

func unorderedEncoderCodecs() []bindingCodec[any] {
	// Match ConfigStd except for key sorting. jjs-go now matches the standard
	// library and therefore belongs in the sorted Generic benchmark instead.
	sonicUnordered := sonic.Config{
		EscapeHTML:       true,
		CompactMarshaler: true,
		CopyString:       true,
		ValidateString:   true,
	}.Froze()
	codecs := []bindingCodec[any]{
		{name: "goccy", marshal: func(value any) ([]byte, error) {
			return goccyjson.MarshalWithOption(value, goccyjson.UnorderedMap())
		}},
		{name: "sonic", marshal: sonicUnordered.Marshal},
	}
	if jsonV2Available {
		codecs = append(codecs, bindingCodec[any]{
			name:    "stdlib-jsonv2",
			marshal: jsonV2Marshal,
		})
	}
	return codecs
}

func BenchmarkEncoder_UnorderedGeneric(b *testing.B) {
	for _, fixture := range []struct {
		name  string
		value any
		size  int
	}{
		{"Small", SmallGeneric, len(SmallJSON)},
		{"Medium", MediumGeneric, len(MediumJSON)},
		{"Large", LargeGeneric, len(LargeJSON)},
	} {
		b.Run(fixture.name, func(b *testing.B) {
			benchmarkBindingEncode(b, unorderedEncoderCodecs(), fixture.value, fixture.size)
		})
	}
}

func TestUnorderedEncoderSemantics(t *testing.T) {
	for _, value := range []any{
		SmallGeneric, MediumGeneric, LargeGeneric,
		map[string]any{"html": "<>&", "invalid": "\xff", "separators": "\u2028\u2029", "nested": []any{nil, true, 1.25}},
	} {
		standard, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		var want any
		if err := json.Unmarshal(standard, &want); err != nil {
			t.Fatal(err)
		}
		for _, codec := range unorderedEncoderCodecs() {
			data, err := codec.marshal(value)
			if err != nil {
				t.Fatalf("%s: %v", codec.name, err)
			}
			var got any
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("%s: unordered encoding changed values", codec.name)
			}
		}
	}
}
