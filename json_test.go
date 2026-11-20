package jjs_go

import (
	"bytes"
	"encoding/json"
	"math"
	"reflect"
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

func staticMarshall(j *JsonT2, bytes *[]byte) {
	*bytes = append(*bytes, "{\"Uid1\" : "...)
	*bytes = append(*bytes, j.Uid1...)
	*bytes = append(*bytes, ',')
	*bytes = append(*bytes, "{\"Uid4\" : "...)
	*bytes = append(*bytes, j.Uid4...)
	*bytes = append(*bytes, ',')
	*bytes = append(*bytes, "{\"UID5\" : "...)
	*bytes = append(*bytes, j.UID5...)
	*bytes = append(*bytes, '}')
	*bytes = append(*bytes, ',')
	*bytes = append(*bytes, "{\"MyUidUIdUUUUUIIIIIIDDDDDD222323827873HHHH5555634ggfwe\" : "...)
	*bytes = append(*bytes, j.UID5...)
	*bytes = append(*bytes, '}')
}
