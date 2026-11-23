package jjs_go

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
)

func TestOptimizedStringPaths(t *testing.T) {
	for _, prefix := range []string{"", "中文", strings.Repeat("ascii", 120), strings.Repeat("中", 180)} {
		for _, suffix := range []string{"", "\n", "\"\\", "\u2028\u2029", "\xff\xfe", "a\x00b", "\xf0\x9f\x98\x80"} {
			input := prefix + suffix + strings.Repeat("tail", 90)
			got, err := Marshal(input)
			if err != nil {
				t.Fatal(err)
			}
			want, err := json.Marshal(input)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("marshal %q: got %s, want %s", input, got, want)
			}
			checkDecodeAgainstStandard(t, string(want), func() string { return "" })
			// Raw invalid UTF-8 is accepted and replaced during JSON decoding.
			raw := `"` + prefix + "\xff" + `\n` + strings.Repeat("tail", 90) + `"`
			checkDecodeAgainstStandard(t, raw, func() string { return "" })
		}
	}
}

func TestOptimizedIntegerBoundaries(t *testing.T) {
	for _, value := range []any{int(0), int8(0), int16(0), int32(0), int64(0), uint(0), uint8(0), uint16(0), uint32(0), uint64(0), uintptr(0)} {
		for _, input := range []string{
			"0", "-0", "1", "-1", "127", "128", "-128", "-129", "255", "256",
			"32767", "32768", "-32768", "-32769", "65535", "65536",
			"2147483647", "2147483648", "-2147483648", "-2147483649", "4294967295", "4294967296",
			"9223372036854775807", "9223372036854775808", "-9223372036854775808", "-9223372036854775809",
			"18446744073709551615", "18446744073709551616", "999999999999999999999999999999999",
			"", " ", "-", "+1", "--1", "01", "-01", "1.0", "1e0", "1e", "1e+", "1x", " 42 \n", "null",
		} {
			got, want := reflect.New(reflect.TypeOf(value)), reflect.New(reflect.TypeOf(value))
			gotErr := Unmarshal([]byte(input), got.Interface())
			wantErr := json.Unmarshal([]byte(input), want.Interface())
			if (gotErr == nil) != (wantErr == nil) {
				t.Fatalf("%T %q: error %v, want %v", value, input, gotErr, wantErr)
			}
			if gotErr == nil && !reflect.DeepEqual(got.Elem().Interface(), want.Elem().Interface()) {
				t.Fatalf("%T %q: got %v, want %v", value, input, got.Elem(), want.Elem())
			}
		}
	}
}

func TestMixedOptionalFieldOrder(t *testing.T) {
	type mixed struct {
		First   string        `json:"first,omitempty"`
		ID      int           `json:"id"`
		Middle  string        `json:"middle,omitempty"`
		Nested  hotpathRecord `json:"nested"`
		Last    string        `json:"last,omitempty"`
		Pointer *int          `json:"pointer"`
	}
	for mask := 0; mask < 16; mask++ {
		value := mixed{ID: 42, Nested: hotpathRecord{ID: 7, Name: "nested"}}
		if mask&1 != 0 {
			value.First = "first"
		}
		if mask&2 != 0 {
			value.Middle = "middle"
		}
		if mask&4 != 0 {
			value.Last = "last"
		}
		if mask&8 != 0 {
			value.Pointer = &value.ID
		}
		got, err := Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		want, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("mask %d: got %s, want %s", mask, got, want)
		}
	}
}

func TestMarshalToReusesBuffer(t *testing.T) {
	storage := make([]byte, 1024)
	for _, value := range []any{nil, 42, "text", hotpathRecord{ID: 7}, map[string]any{"a": true}, []byte{1, 2, 3}} {
		got, err := MarshalTo(storage[:12], value)
		if err != nil {
			t.Fatal(err)
		}
		want, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("got %s, want %s", got, want)
		}
		if &got[0] != &storage[0] {
			t.Fatal("buffer with enough capacity was replaced")
		}
	}
	got, err := MarshalTo(storage[:1], strings.Repeat("grow", 1024))
	if err != nil || !json.Valid(got) {
		t.Fatalf("buffer growth failed: %v", err)
	}
	if got, err := MarshalTo(storage, make(chan int)); err == nil || got != nil {
		t.Fatal("expected encoding error")
	}
}

func TestNestedInterfaceContextRestoration(t *testing.T) {
	value := []any{
		map[string]any{"nested": []any{42, true, "中文", 3.5, hotpathRecord{ID: 7}}},
		hotpathRecord{ID: 8, Note: "after nested map"},
		[]any{&jitChild{Label: "pointer", Count: 9}, jitCustomValue{Value: "custom"}, gcMarshaler{}},
	}
	got, err := Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	want, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var a, b any
	if err := json.Unmarshal(got, &a); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(want, &b); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("got %s, want %s", got, want)
	}
}

type gcMarshaler struct{}

func (gcMarshaler) MarshalJSON() ([]byte, error) {
	runtime.GC()
	return []byte(`"gc"`), nil
}

func TestConcurrentColdTypes(t *testing.T) {
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		typ := reflect.StructOf([]reflect.StructField{
			{Name: "ID", Type: reflect.TypeOf(int(0)), Tag: reflect.StructTag(fmt.Sprintf(`json:"cold%d"`, i))},
			{Name: "Name", Type: reflect.TypeOf("")},
		})
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			value := reflect.New(typ)
			value.Elem().Field(0).SetInt(42)
			value.Elem().Field(1).SetString("cold")
			got, err := Marshal(value.Interface())
			if err != nil {
				t.Error(err)
				return
			}
			want, err := json.Marshal(value.Interface())
			if err != nil {
				t.Error(err)
				return
			}
			if !bytes.Equal(got, want) {
				t.Errorf("got %s, want %s", got, want)
				return
			}
			target := reflect.New(typ)
			if err := Unmarshal(got, target.Interface()); err != nil {
				t.Error(err)
				return
			}
			if !reflect.DeepEqual(value.Interface(), target.Interface()) {
				t.Error("concurrent cold round trip mismatch")
			}
		}()
	}
	close(start)
	wg.Wait()
}

func TestWideStructFieldOrder(t *testing.T) {
	fields := make([]reflect.StructField, 32)
	for i := range fields {
		fields[i] = reflect.StructField{Name: fmt.Sprintf("Field%02d", i), Type: reflect.TypeOf(int(0))}
	}
	typ := reflect.StructOf(fields)
	for _, reverse := range []bool{false, true} {
		for _, folded := range []bool{false, true} {
			var input strings.Builder
			input.WriteString(`{"unknown":{"nested":[1,2,3]}`)
			for i := range fields {
				index := i
				if reverse {
					index = len(fields) - 1 - i
				}
				name := fields[index].Name
				if folded {
					name = strings.ToLower(name)
				}
				fmt.Fprintf(&input, ",%q:%d", name, index)
			}
			input.WriteString(`,"Field01":99,"field01":101,"Field31":-42}`)
			got, want := reflect.New(typ), reflect.New(typ)
			if err := Unmarshal([]byte(input.String()), got.Interface()); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal([]byte(input.String()), want.Interface()); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got.Interface(), want.Interface()) {
				t.Fatalf("reverse=%t folded=%t: got %v, want %v", reverse, folded, got, want)
			}
		}
	}
}

func BenchmarkMarshalTo(b *testing.B) {
	value := &hotpathRecord{ID: 42, Name: "record", Note: "reuse"}
	dst, err := MarshalTo(nil, value)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		dst, err = MarshalTo(dst, value)
		if err != nil {
			b.Fatal(err)
		}
	}
	hotpathBytes = dst
}
