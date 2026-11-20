package jjs_go

import (
	"bytes"
	"encoding/json"
	"math"
	"reflect"
	"sync"
	"testing"
	"time"
	"unicode/utf8"
)

type codecNested struct {
	Label string `json:"label"`
	Count int    `json:"count"`
}

type codecFixture struct {
	Name    string          `json:"name"`
	Empty   string          `json:"empty,omitempty"`
	Age     int16           `json:"age"`
	Score   float64         `json:"score"`
	Active  bool            `json:"active"`
	Tags    []string        `json:"tags"`
	Meta    map[string]int  `json:"meta"`
	Nested  *codecNested    `json:"nested"`
	Bytes   []byte          `json:"bytes"`
	Array   [2]int          `json:"array"`
	Raw     json.RawMessage `json:"raw"`
	Number  json.Number     `json:"number"`
	At      time.Time       `json:"at"`
	Ignored string          `json:"-"`
	private string
}

type codecText string

func (v codecText) MarshalText() ([]byte, error) {
	return []byte("text:" + string(v)), nil
}

type codecEmbedded struct {
	codecNested
	Other string
}

type codecPointerMarshaler struct {
	Value string
}

func (v *codecPointerMarshaler) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]string{"pointer": v.Value})
}

type codecPointerHolder struct {
	Item codecPointerMarshaler `json:"item"`
}

func fixtureValue() codecFixture {
	return codecFixture{
		Name:    "hello <world> & 中文",
		Age:     17,
		Score:   1.2345678901234567,
		Active:  true,
		Tags:    []string{"go", "", "\n"},
		Meta:    map[string]int{"z": 3, "a": 1, "中": 2},
		Nested:  &codecNested{Label: "inner", Count: 9},
		Bytes:   []byte{0, 1, 2, 250},
		Array:   [2]int{4, 5},
		Raw:     json.RawMessage(`{"raw":true}`),
		Number:  json.Number("12345678901234567890"),
		At:      time.Date(2026, 11, 21, 12, 30, 0, 0, time.UTC),
		Ignored: "not encoded",
		private: "not encoded",
	}
}

func TestMarshalMatchesEncodingJSON(t *testing.T) {
	fixture := fixtureValue()
	values := []any{
		nil,
		"plain",
		"quote\" slash\\ newline\n tab\t html<>& unicode\u2028",
		string([]byte{'a', 0xff, 'b'}),
		true,
		int64(math.MaxInt64),
		uint64(math.MaxUint64),
		0.0,
		-0.0,
		1e-9,
		1e21,
		1.2345678901234567,
		[]int{1, 2, 3},
		[]int(nil),
		[2]int{1, 2},
		[]byte("binary"),
		[]byte(nil),
		map[string]any{"b": 2, "a": 1},
		codecText("value"),
		json.Number("12345678901234567890"),
		&fixture,
		fixture,
		codecEmbedded{codecNested: codecNested{Label: "embedded", Count: 2}, Other: "field"},
		&codecPointerHolder{Item: codecPointerMarshaler{Value: "method"}},
		codecPointerHolder{Item: codecPointerMarshaler{Value: "method"}},
	}

	for _, value := range values {
		value := value
		name := "nil"
		if typ := reflect.TypeOf(value); typ != nil {
			name = typ.String()
		}
		t.Run(name, func(t *testing.T) {
			got, err := Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			want, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("Marshal mismatch\ngot:  %s\nwant: %s", got, want)
			}
			if !json.Valid(got) {
				t.Fatalf("Marshal returned invalid JSON: %q", got)
			}
		})
	}
}

func TestMarshalErrors(t *testing.T) {
	_, err := Marshal(math.Inf(1))
	if err == nil {
		t.Fatal("expected an error for infinity")
	}
	_, err = Marshal(make(chan int))
	if err == nil {
		t.Fatal("expected an error for channel")
	}
	_, err = Marshal(json.Number("null"))
	if err == nil {
		t.Fatal("expected an error for an invalid JSON number")
	}

	type node struct {
		Next *node `json:"next"`
	}
	value := &node{}
	value.Next = value
	_, err = Marshal(value)
	if err == nil {
		t.Fatal("expected a cycle error")
	}
}

func TestUnmarshalRoundTrip(t *testing.T) {
	input := fixtureValue()
	input.Ignored = ""
	input.private = ""
	encoded, err := Marshal(&input)
	if err != nil {
		t.Fatal(err)
	}

	var output codecFixture
	if err := Unmarshal(encoded, &output); err != nil {
		t.Fatal(err)
	}
	output.At = output.At.UTC()
	if !reflect.DeepEqual(output, input) {
		t.Fatalf("round trip mismatch\ngot:  %#v\nwant: %#v", output, input)
	}
}

func TestUnmarshalJSONSemantics(t *testing.T) {
	type payload struct {
		Name   string         `json:"name"`
		Count  int64          `json:"count"`
		Values []float64      `json:"values"`
		Meta   map[string]int `json:"meta"`
		Next   *codecNested   `json:"next"`
	}

	var output payload
	input := []byte(`{"name":"中文","count":9007199254740993,"values":[1,2.5],"meta":{"b":2,"a":1},"next":{"label":"x","count":3}}`)
	if err := Unmarshal(input, &output); err != nil {
		t.Fatal(err)
	}
	want := payload{
		Name:   "中文",
		Count:  9007199254740993,
		Values: []float64{1, 2.5},
		Meta:   map[string]int{"a": 1, "b": 2},
		Next:   &codecNested{Label: "x", Count: 3},
	}
	if !reflect.DeepEqual(output, want) {
		t.Fatalf("got %#v, want %#v", output, want)
	}

	var pointer *codecNested
	if err := Unmarshal([]byte("null"), &pointer); err != nil {
		t.Fatal(err)
	}
	if pointer != nil {
		t.Fatalf("null did not clear pointer: %#v", pointer)
	}
}

func TestUnmarshalErrors(t *testing.T) {
	if err := Unmarshal([]byte(`{}`), nil); err == nil {
		t.Fatal("expected an invalid target error")
	}
	var value codecNested
	if err := Unmarshal([]byte(`{}`), value); err == nil {
		t.Fatal("expected a non-pointer target error")
	}
	if err := Unmarshal([]byte(`{"label":`), &value); err == nil {
		t.Fatal("expected a syntax error")
	}
	if err := Unmarshal([]byte(`{"count":"not-an-int"}`), &value); err == nil {
		t.Fatal("expected a type error")
	}
}

func TestConcurrentCodecUse(t *testing.T) {
	input := fixtureValue()
	want, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	errs := make(chan error, 32)
	for worker := 0; worker < 32; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				got, err := Marshal(input)
				if err != nil {
					errs <- err
					return
				}
				if !bytes.Equal(got, want) {
					errs <- &mismatchError{got: got, want: want}
					return
				}
				var decoded codecFixture
				if err := Unmarshal(got, &decoded); err != nil {
					errs <- err
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
}

type mismatchError struct {
	got  []byte
	want []byte
}

func (e *mismatchError) Error() string {
	return "concurrent marshal mismatch: got " + string(e.got) + ", want " + string(e.want)
}

func BenchmarkMarshal(b *testing.B) {
	value := fixtureValue()
	b.Run("jjs", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if _, err := Marshal(value); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("encoding/json", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if _, err := json.Marshal(value); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkUnmarshal(b *testing.B) {
	data, err := json.Marshal(fixtureValue())
	if err != nil {
		b.Fatal(err)
	}
	b.Run("jjs", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			var value codecFixture
			if err := Unmarshal(data, &value); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("encoding/json", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			var value codecFixture
			if err := json.Unmarshal(data, &value); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func FuzzMarshalString(f *testing.F) {
	for _, seed := range []string{
		"",
		"plain",
		"\x00\n\t<>&\\\"",
		"中文😀",
		string([]byte{0xff, 0xfe}),
		"\u2028\u2029",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, value string) {
		got, err := Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		want, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("got %q, want %q", got, want)
		}
		var decoded string
		if err := Unmarshal(got, &decoded); err != nil {
			t.Fatal(err)
		}
		if utf8.ValidString(value) && decoded != value {
			t.Fatalf("round trip mismatch: got %q, want %q", decoded, value)
		}
	})
}

func FuzzMarshalScalars(f *testing.F) {
	f.Add(int64(-42), uint64(42), 1.25, true)
	f.Add(int64(math.MinInt64), uint64(math.MaxUint64), math.SmallestNonzeroFloat64, false)
	f.Fuzz(func(t *testing.T, signed int64, unsigned uint64, floating float64, boolean bool) {
		values := []any{signed, unsigned, boolean}
		if math.IsNaN(floating) || math.IsInf(floating, 0) {
			if _, err := Marshal(floating); err == nil {
				t.Fatal("expected an error for a non-finite float")
			}
		} else {
			values = append(values, floating)
		}
		for _, value := range values {
			got, err := Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			want, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("got %q, want %q for %T(%v)", got, want, value, value)
			}
		}
	})
}
