package jjs_go

import (
	"encoding/json"
	"math"
	"reflect"
	"testing"
)

func checkDecodeAgainstStandard[T any](t *testing.T, input string, initial func() T) {
	t.Helper()
	got, want := initial(), initial()
	gotErr := Unmarshal([]byte(input), &got)
	wantErr := json.Unmarshal([]byte(input), &want)
	if (gotErr == nil) != (wantErr == nil) {
		t.Fatalf("input %s: error = %v, standard error = %v", input, gotErr, wantErr)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("input %s: got %#v (%T), want %#v (%T)", input, got, got, want, want)
	}
}

func TestMarshalNestedDynamicFieldOffsets(t *testing.T) {
	type child struct {
		Value int `json:"value,omitempty"`
	}
	type parent struct {
		Pad   int   `json:"pad"`
		Child child `json:"child"`
	}
	for _, value := range []any{
		parent{42, child{7}},
		parent{42, child{}},
		[2]parent{{42, child{7}}, {19, child{3}}},
		struct{ Pad, Nested parent }{parent{1, child{2}}, parent{3, child{4}}},
	} {
		got, err := Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		want, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != string(want) {
			t.Errorf("%T: got %s, want %s", value, got, want)
		}
	}
}

func TestMarshalIntegerMapKeys(t *testing.T) {
	type namedInt int16
	for _, value := range []any{
		map[int]string{1: "one", -2: "two"},
		map[int8]int{-128: 1}, map[int16]int{-32768: 1},
		map[int32]int{math.MinInt32: 1}, map[int64]int{math.MinInt64: 1},
		map[uint]int{42: 1}, map[uint8]int{255: 1},
		map[uint16]int{65535: 1}, map[uint32]int{math.MaxUint32: 1},
		map[uint64]int{math.MaxUint64: 1}, map[uintptr]int{42: 1},
		map[namedInt]int{-7: 1},
	} {
		got, err := Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		want, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		var gotValue, wantValue any
		if err := json.Unmarshal(got, &gotValue); err != nil {
			t.Errorf("%T: invalid JSON %q: %v", value, got, err)
			continue
		}
		if err := json.Unmarshal(want, &wantValue); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(gotValue, wantValue) {
			t.Errorf("%T: got %s, want %s", value, got, want)
		}
	}
	for _, value := range []any{map[bool]int{true: 1}, map[float64]int{1: 1}} {
		if data, err := Marshal(value); err == nil {
			t.Errorf("%T: accepted unsupported key type: %s", value, data)
		}
	}
}

func TestUnmarshalArrayClearsTail(t *testing.T) {
	for _, input := range []string{`[]`, `[3]`, `[3,4,5,6]`, `null`} {
		checkDecodeAgainstStandard(t, input, func() [3]int { return [3]int{7, 8, 9} })
	}
	checkDecodeAgainstStandard(t, `["new"]`, func() [3]*string {
		s := "old"
		return [3]*string{&s, &s, &s}
	})
}

func TestUnmarshalMapValuesStartZero(t *testing.T) {
	type pair struct{ A, B int }
	for _, input := range []string{`{"k":{"A":2}}`, `{"k":null}`, `{"k":{"B":3},"k":{"A":2}}`} {
		checkDecodeAgainstStandard(t, input, func() map[string]pair {
			return map[string]pair{"k": {1, 9}, "keep": {5, 6}}
		})
		checkDecodeAgainstStandard(t, input, func() map[string]*pair {
			return map[string]*pair{"k": {1, 9}, "keep": {5, 6}}
		})
	}
	checkDecodeAgainstStandard(t, `{"k":"new"}`, func() map[string]any {
		return map[string]any{"k": 42}
	})
}

func TestUnmarshalPopulatedInterface(t *testing.T) {
	for _, input := range []string{`2`, `"new"`, `true`, `{"A":2}`, `[1]`, `null`} {
		for _, initial := range []func() any{
			func() any { return int(1) },
			func() any { return "old" },
			func() any { return map[string]any{"old": true} },
			func() any { return (*int)(nil) },
		} {
			checkDecodeAgainstStandard(t, input, initial)
		}
	}
	type pair struct{ A, B int }
	value := &pair{1, 9}
	var target any = value
	if err := Unmarshal([]byte(`{"A":2}`), &target); err != nil {
		t.Fatal(err)
	}
	if target != value || *value != (pair{2, 9}) {
		t.Fatalf("existing pointer was replaced or not updated: %#v, %#v", target, value)
	}
	for _, initial := range []func() jitStringer{
		func() jitStringer { return &jitNamed{Name: "old"} },
		func() jitStringer { return jitNamed{Name: "old"} },
		func() jitStringer { return (*jitNamed)(nil) },
		func() jitStringer { return nil },
	} {
		checkDecodeAgainstStandard(t, `{"name":"new"}`, initial)
	}
}

func TestUnmarshalFoldedFieldNames(t *testing.T) {
	type fields struct {
		Lower int `json:"name"`
		Upper int `json:"NAME"`
		K     int `json:"K"`
		S     int `json:"S"`
	}
	for _, input := range []string{
		`{"NAME":2}`, `{"NaMe":3}`, `{"name":1,"NAME":2}`,
		`{"K":4,"ſ":5}`, `{"unknown":1}`, `{"NaMe":1,"name":2}`,
	} {
		checkDecodeAgainstStandard(t, input, func() fields { return fields{} })
	}
}

func TestUnmarshalByteSlices(t *testing.T) {
	type namedBytes []byte
	type octet uint8
	for _, input := range []string{`"AQID"`, `""`, `null`, `[1,2,255]`, `"A\u0051ID"`, `"bad!"`} {
		checkDecodeAgainstStandard(t, input, func() []byte { return []byte{9, 8} })
		checkDecodeAgainstStandard(t, input, func() namedBytes { return namedBytes{9, 8} })
		checkDecodeAgainstStandard(t, input, func() []octet { return []octet{9, 8} })
	}
	type holder struct{ Data []byte }
	for _, value := range []holder{{nil}, {[]byte{}}, {[]byte{0, 1, 255}}} {
		data, err := Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		checkDecodeAgainstStandard(t, string(data), func() holder { return holder{} })
	}
}
