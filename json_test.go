package jjs_go

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"strings"
	"sync"
	"testing"
)

type jitChild struct {
	Label string `json:"label"`
	Count int    `json:"count"`
}

type jitFixture struct {
	Name   string         `json:"name"`
	Empty  string         `json:"empty"`
	Age    int16          `json:"age"`
	Score  float64        `json:"score"`
	Active bool           `json:"active"`
	Child  jitChild       `json:"child"`
	Ptr    *jitChild      `json:"ptr"`
	Values [3]int32       `json:"values"`
	Tags   []string       `json:"tags"`
	Meta   map[string]int `json:"meta"`
	Any    any            `json:"any"`
}

type jitStringer interface {
	StringValue() string
}

type jitNamed struct {
	Name string `json:"name"`
}

func (v jitNamed) StringValue() string {
	return v.Name
}

type jitInterfaceFixture struct {
	Empty   any         `json:"empty"`
	NonZero jitStringer `json:"non_zero"`
}

type JITEmbeddedBase struct {
	ID      int    `json:"id"`
	Ignored string `json:"-"`
	Empty   string `json:"empty,omitempty"`
}

type jitEmbeddedValue struct {
	JITEmbeddedBase
	Name string `json:"name"`
}

type jitEmbeddedPointer struct {
	*JITEmbeddedBase
	Name string `json:"name"`
}

type jitNamedEmbedded struct {
	JITEmbeddedBase `json:"base"`
}

type jitNamedEmbeddedPointer struct {
	*JITEmbeddedBase `json:"base"`
}

type jitIgnoredEmbedded struct {
	JITEmbeddedBase `json:"-"`
	Name            string `json:"name"`
}

type jitEmbeddedConflictA struct {
	Value int `json:"value"`
}

type jitEmbeddedConflictB struct {
	Value int `json:"value"`
}

type jitEmbeddedConflict struct {
	jitEmbeddedConflictA
	jitEmbeddedConflictB
	Name string `json:"name"`
}

type jitOmitEmptyFixture struct {
	Bool      bool           `json:"bool,omitempty"`
	Int       int            `json:"int,omitempty"`
	Uint      uint           `json:"uint,omitempty"`
	Float     float64        `json:"float,omitempty"`
	String    string         `json:"string,omitempty"`
	Slice     []string       `json:"slice,omitempty"`
	Map       map[string]int `json:"map,omitempty"`
	Pointer   *int           `json:"pointer,omitempty"`
	Interface any            `json:"interface,omitempty"`
	Array     [0]int         `json:"array,omitempty"`
	Struct    struct{}       `json:"struct,omitempty"`
}

type jitCustomValue struct {
	Value string
}

func (v jitCustomValue) MarshalJSON() ([]byte, error) {
	return json.Marshal("custom:" + v.Value)
}

func (v *jitCustomValue) UnmarshalJSON(data []byte) error {
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	v.Value = value[len("custom:"):]
	return nil
}

type jitCustomCodec interface {
	json.Marshaler
	json.Unmarshaler
	CodecName() string
}

type jitNamedCodec struct {
	Value string
}

func (v jitNamedCodec) MarshalJSON() ([]byte, error) {
	return json.Marshal("named:" + v.Value)
}

func (v *jitNamedCodec) UnmarshalJSON(data []byte) error {
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	v.Value = value[len("named:"):]
	return nil
}

func (jitNamedCodec) CodecName() string {
	return "named"
}

type jitCustomError struct{}

func (jitCustomError) MarshalJSON() ([]byte, error) {
	return nil, errors.New("custom marshal failure")
}

type jitCustomInvalid struct{}

func (jitCustomInvalid) MarshalJSON() ([]byte, error) {
	return []byte(`{invalid`), nil
}

type jitPointerOnlyValue struct {
	Value string
}

func (v *jitPointerOnlyValue) MarshalJSON() ([]byte, error) {
	return json.Marshal("pointer-only:" + v.Value)
}

func (v *jitPointerOnlyValue) UnmarshalJSON(data []byte) error {
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	v.Value = value[len("pointer-only:"):]
	return nil
}

type jitCustomUnmarshalError struct{}

func (*jitCustomUnmarshalError) UnmarshalJSON([]byte) error {
	return errors.New("custom unmarshal failure")
}

type jitCustomHTML struct{}

func (jitCustomHTML) MarshalJSON() ([]byte, error) {
	return []byte(" { \"html\" : \"<&>\" } "), nil
}

func jitFixtureValue() jitFixture {
	return jitFixture{
		Name:   "hello <world> & 中文",
		Empty:  "",
		Age:    17,
		Score:  1.2345678901234567,
		Active: true,
		Child:  jitChild{Label: "inner", Count: -9},
		Ptr:    &jitChild{Label: "pointer", Count: 7},
		Values: [3]int32{1, -2, 3},
		Tags:   []string{"go", "jit"},
		Meta:   map[string]int{"answer": 42},
		Any:    jitChild{Label: "dynamic", Count: 5},
	}
}

func TestMarshalJITMatchesEncodingJSON(t *testing.T) {
	values := []any{
		"quote\" slash\\ newline\n tab\t html<>& unicode\u2028",
		string([]byte{'a', 0xff, 'b'}),
		true,
		int64(math.MinInt64),
		uint64(math.MaxUint64),
		1e-9,
		1e21,
		1.2345678901234567,
		[3]int32{1, -2, 3},
		jitFixtureValue(),
	}
	for _, value := range values {
		got, err := Marshal(value)
		if err != nil {
			t.Fatalf("%T: %v", value, err)
		}
		want, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("%T mismatch\ngot:  %s\nwant: %s", value, got, want)
		}
	}
}

func TestUnmarshalJIT(t *testing.T) {
	input := jitFixtureValue()
	data, err := Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	var output jitFixture
	output.Any = jitChild{}
	if err := Unmarshal(data, &output); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(output, input) {
		t.Fatalf("round trip mismatch\ngot:  %#v\nwant: %#v", output, input)
	}
}

func TestUnmarshalJITScalarsAndArrays(t *testing.T) {
	var text string
	if err := Unmarshal([]byte(`"hello\n\u4e16\u754c"`), &text); err != nil {
		t.Fatal(err)
	}
	if text != "hello\n世界" {
		t.Fatalf("unexpected string: %q", text)
	}

	var number int64
	if err := Unmarshal([]byte(`-9223372036854775808`), &number); err != nil {
		t.Fatal(err)
	}
	if number != math.MinInt64 {
		t.Fatalf("unexpected int: %d", number)
	}

	var values [2]uint16
	if err := Unmarshal([]byte(`[1,65535]`), &values); err != nil {
		t.Fatal(err)
	}
	if values != [2]uint16{1, 65535} {
		t.Fatalf("unexpected array: %v", values)
	}
}

func TestUnmarshalJITStringPaths(t *testing.T) {
	inputs := [][]byte{
		[]byte(`"short"`),
		[]byte(`"` + strings.Repeat("a", 80) + `"`),
		[]byte(`"<html>&` + strings.Repeat("b", 80) + `"`),
		[]byte(`"中文-` + strings.Repeat("世界", 30) + `"`),
		[]byte(`"prefix-` + strings.Repeat("c", 40) + `\n\u4e16\u754c"`),
		append(append([]byte{'"', 'a'}, 0xff), '"'),
	}
	for _, input := range inputs {
		var got, want string
		if err := Unmarshal(input, &got); err != nil {
			t.Fatalf("Unmarshal(%q): %v", input, err)
		}
		if err := json.Unmarshal(input, &want); err != nil {
			t.Fatalf("encoding/json.Unmarshal(%q): %v", input, err)
		}
		if got != want {
			t.Fatalf("Unmarshal(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestJITInterfaceDispatch(t *testing.T) {
	input := jitInterfaceFixture{
		Empty:   jitNamed{Name: "empty-interface"},
		NonZero: jitNamed{Name: "non-empty-interface"},
	}
	data, err := Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	want, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, want) {
		t.Fatalf("got %s, want %s", data, want)
	}

	output := jitInterfaceFixture{
		Empty:   jitNamed{},
		NonZero: jitNamed{},
	}
	if err := Unmarshal(data, &output); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(output, input) {
		t.Fatalf("interface dispatch mismatch\ngot:  %#v\nwant: %#v", output, input)
	}
}

func TestJITInterfacePointerRoundTrip(t *testing.T) {
	input := struct {
		Value any `json:"value"`
	}{
		Value: &jitChild{Label: "pointer", Count: 7},
	}
	data, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	output := struct {
		Value any `json:"value"`
	}{
		Value: &jitChild{},
	}
	if err := Unmarshal(data, &output); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(output.Value, input.Value) {
		t.Fatalf("got %#v, want %#v", output.Value, input.Value)
	}
}

func TestMarshalJITInterfaceContainingMap(t *testing.T) {
	values := []any{struct {
		Payload any `json:"payload"`
	}{
		Payload: map[string]any{
			"name":   "nested",
			"values": []any{true, float64(7)},
		},
	}, map[string]any{
		"name":   "root",
		"values": []any{false, float64(8)},
	}}
	for _, value := range values {
		data, err := Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		var got, want any
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatal(err)
		}
		wantData, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(wantData, &want); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("got %s, want %s", data, wantData)
		}
	}
}

func TestJITFieldsAndOmitEmptyMatchEncodingJSON(t *testing.T) {
	value := 0
	var typedNil *int
	values := []any{
		jitEmbeddedValue{
			JITEmbeddedBase: JITEmbeddedBase{ID: 1},
			Name:            "value",
		},
		jitEmbeddedPointer{
			JITEmbeddedBase: &JITEmbeddedBase{ID: 2},
			Name:            "pointer",
		},
		jitEmbeddedPointer{Name: "nil-pointer"},
		jitNamedEmbedded{
			JITEmbeddedBase: JITEmbeddedBase{ID: 3, Empty: "included"},
		},
		jitNamedEmbeddedPointer{
			JITEmbeddedBase: &JITEmbeddedBase{ID: 5},
		},
		jitIgnoredEmbedded{
			JITEmbeddedBase: JITEmbeddedBase{ID: 4},
			Name:            "ignored-embedded",
		},
		jitEmbeddedConflict{
			jitEmbeddedConflictA: jitEmbeddedConflictA{Value: 1},
			jitEmbeddedConflictB: jitEmbeddedConflictB{Value: 2},
			Name:                 "conflict",
		},
		jitOmitEmptyFixture{},
		jitOmitEmptyFixture{
			Float: math.Copysign(0, -1),
			Slice: []string{},
			Map:   map[string]int{},
		},
		jitOmitEmptyFixture{Interface: typedNil},
		jitOmitEmptyFixture{
			Bool:      true,
			Int:       1,
			Uint:      2,
			Float:     3.5,
			String:    "included",
			Slice:     []string{"x"},
			Map:       map[string]int{"x": 1},
			Pointer:   &value,
			Interface: 4,
		},
	}
	for _, value := range values {
		got, err := Marshal(value)
		if err != nil {
			t.Fatalf("%T marshal: %v", value, err)
		}
		want, err := json.Marshal(value)
		if err != nil {
			t.Fatalf("%T encoding/json marshal: %v", value, err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("%T marshal mismatch\ngot:  %s\nwant: %s", value, got, want)
		}

		target := reflect.New(reflect.TypeOf(value)).Interface()
		if err := Unmarshal(want, target); err != nil {
			t.Fatalf("%T unmarshal: %v", value, err)
		}
		var standard any = reflect.New(reflect.TypeOf(value)).Interface()
		if err := json.Unmarshal(want, standard); err != nil {
			t.Fatalf("%T encoding/json unmarshal: %v", value, err)
		}
		gotValue := reflect.ValueOf(target).Elem().Interface()
		wantValue := reflect.ValueOf(standard).Elem().Interface()
		if !reflect.DeepEqual(gotValue, wantValue) {
			t.Fatalf("%T unmarshal mismatch\ngot:  %#v\nwant: %#v", value, gotValue, wantValue)
		}
	}
}

func TestJITCustomMarshalerAndUnmarshaler(t *testing.T) {
	value := jitCustomValue{Value: "value"}
	pointer := &jitCustomValue{Value: "pointer"}
	named := &jitNamedCodec{Value: "interface"}
	raw := json.RawMessage(`{"nested":[1,2,3]}`)
	holder := struct {
		Value customField[jitCustomValue] `json:"value"`
		Ptr   *jitCustomValue             `json:"ptr"`
		Named jitCustomCodec              `json:"named"`
		OnlyP *jitPointerOnlyValue        `json:"only_ptr"`
		Raw   json.RawMessage             `json:"raw"`
	}{
		Value: customField[jitCustomValue]{Value: value},
		Ptr:   pointer,
		Named: named,
		OnlyP: &jitPointerOnlyValue{Value: "pointer"},
		Raw:   raw,
	}
	got, err := Marshal(holder)
	if err != nil {
		t.Fatal(err)
	}
	want, err := json.Marshal(holder)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("marshal mismatch\ngot:  %s\nwant: %s", got, want)
	}

	roundTrip := struct {
		Value customField[jitCustomValue] `json:"value"`
		Ptr   *jitCustomValue             `json:"ptr"`
		Named jitCustomCodec              `json:"named"`
		OnlyP *jitPointerOnlyValue        `json:"only_ptr"`
		Raw   json.RawMessage             `json:"raw"`
	}{
		Named: &jitNamedCodec{},
		OnlyP: &jitPointerOnlyValue{},
	}
	if err := Unmarshal(want, &roundTrip); err != nil {
		t.Fatal(err)
	}
	var standard struct {
		Value customField[jitCustomValue] `json:"value"`
		Ptr   *jitCustomValue             `json:"ptr"`
		Named jitCustomCodec              `json:"named"`
		OnlyP *jitPointerOnlyValue        `json:"only_ptr"`
		Raw   json.RawMessage             `json:"raw"`
	}
	standard.Named = &jitNamedCodec{}
	standard.OnlyP = &jitPointerOnlyValue{}
	if err := json.Unmarshal(want, &standard); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(roundTrip, standard) {
		t.Fatalf("unmarshal mismatch\ngot:  %#v\nwant: %#v", roundTrip, standard)
	}

	nullHolder := struct {
		Ptr   *jitCustomValue `json:"ptr"`
		Named jitCustomCodec  `json:"named"`
	}{}
	got, err = Marshal(nullHolder)
	if err != nil {
		t.Fatal(err)
	}
	want, err = json.Marshal(nullHolder)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("nil marshal mismatch\ngot:  %s\nwant: %s", got, want)
	}

	pointerOnly := struct {
		Value jitPointerOnlyValue `json:"value"`
	}{Value: jitPointerOnlyValue{Value: "value"}}
	got, err = Marshal(pointerOnly)
	if err != nil {
		t.Fatal(err)
	}
	want, err = json.Marshal(pointerOnly)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("pointer-only value mismatch\ngot:  %s\nwant: %s", got, want)
	}

	pointerRoot := &struct {
		Value jitPointerOnlyValue `json:"value"`
	}{Value: jitPointerOnlyValue{Value: "addressable"}}
	got, err = Marshal(pointerRoot)
	if err != nil {
		t.Fatal(err)
	}
	want, err = json.Marshal(pointerRoot)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("pointer-root custom mismatch\ngot:  %s\nwant: %s", got, want)
	}
}

// customField keeps jitCustomValue a named field while testing value-receiver
// MarshalJSON through the normal field path.
type customField[T any] struct {
	Value T
}

func (v customField[T]) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.Value)
}

func (v *customField[T]) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &v.Value)
}

func TestJITCustomCodecErrors(t *testing.T) {
	if _, err := Marshal(struct {
		Value jitCustomError `json:"value"`
	}{}); err == nil {
		t.Fatal("expected custom MarshalJSON error")
	}
	if _, err := Marshal(struct {
		Value jitCustomInvalid `json:"value"`
	}{}); err == nil {
		t.Fatal("expected invalid MarshalJSON output error")
	}
	if err := Unmarshal([]byte(`{"value":{}}`), &struct {
		Value jitCustomUnmarshalError `json:"value"`
	}{}); err == nil {
		t.Fatal("expected custom UnmarshalJSON error")
	}
	if err := Unmarshal([]byte(`{"value":"text"}`), &struct {
		Value jitCustomCodec `json:"value"`
	}{}); err == nil {
		t.Fatal("expected nil named interface error")
	}

	html := struct {
		Value jitCustomHTML `json:"value"`
	}{}
	got, err := Marshal(html)
	if err != nil {
		t.Fatal(err)
	}
	want, err := json.Marshal(html)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("custom JSON normalization mismatch\ngot:  %s\nwant: %s", got, want)
	}
}

func TestUnmarshalJITNestedMap(t *testing.T) {
	data := []byte(`{"name":"root","nested":{"items":[{"enabled":true},{"count":3}]}}`)
	var got map[string]any
	if err := Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	var want map[string]any
	if err := json.Unmarshal(data, &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestJITConcurrentDynamicMaps(t *testing.T) {
	value := map[string]any{
		"name": "root",
		"nested": map[string]any{
			"items": []any{
				map[string]any{"enabled": true},
				map[string]any{"count": float64(3)},
			},
		},
	}
	want, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 32)
	for worker := 0; worker < 32; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 20; i++ {
				data, err := Marshal(value)
				if err != nil {
					errs <- err
					return
				}
				var got any
				if err := json.Unmarshal(data, &got); err != nil {
					errs <- err
					return
				}
				var expected any
				if err := json.Unmarshal(want, &expected); err != nil {
					errs <- err
					return
				}
				if !reflect.DeepEqual(got, expected) {
					errs <- &jitMismatchError{got: data, want: want}
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

func TestUnmarshalJITErrors(t *testing.T) {
	if err := Unmarshal([]byte(`{}`), nil); err == nil {
		t.Fatal("expected invalid target error")
	}
	var value jitChild
	if err := Unmarshal([]byte(`{"label":`), &value); err == nil {
		t.Fatal("expected syntax error")
	}
	if err := Unmarshal([]byte(`{"count":"bad"}`), &value); err == nil {
		t.Fatal("expected type error")
	}
	var generic any
	if err := Unmarshal([]byte(`{0\":\"\"}`), &generic); err == nil {
		t.Fatal("expected invalid object key error")
	}
}

func TestJITConcurrentUse(t *testing.T) {
	input := jitFixtureValue()
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
					errs <- &jitMismatchError{got: got, want: want}
					return
				}
				var decoded jitFixture
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

type jitMismatchError struct {
	got  []byte
	want []byte
}

func (e *jitMismatchError) Error() string {
	return "JIT output mismatch: got " + string(e.got) + ", want " + string(e.want)
}

func BenchmarkJITMarshal(b *testing.B) {
	value := jitFixtureValue()
	b.Run("jjs-jit", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			if _, err := Marshal(value); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("encoding/json", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			if _, err := json.Marshal(value); err != nil {
				b.Fatal(err)
			}
		}
	})
}
