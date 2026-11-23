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

func TestDecodeResultOwnership(t *testing.T) {
	// Keep earlier results alive while input buffers and pooled decode scratch
	// are reused, including after a GC and from another goroutine.
	var got, want []any
	var input []byte
	for i := 0; i < 128; i++ {
		input = fmt.Appendf(input[:0], `{"text":"value-%d-\n中文","array":["text-%d",%d.25,[],{"key":"value"}],"long":%q}`, i, i, i, strings.Repeat("long", 150))
		var a, b any
		if err := Unmarshal(input, &a); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(input, &b); err != nil {
			t.Fatal(err)
		}
		got, want = append(got, a), append(want, b)
		clear(input)
		if i%16 == 0 {
			runtime.GC()
		}
	}
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				var value any
				if err := Unmarshal([]byte(`{"different":[123.5,"overwrite",{"x":"y"}]}`), &value); err != nil {
					t.Error(err)
				}
			}
		}()
	}
	wg.Wait()
	if !reflect.DeepEqual(got, want) {
		t.Fatal("retained decode results changed after buffer/scratch reuse")
	}
	// Typed string fields use the same ownership rule as generic strings.
	var value struct{ A, B string }
	input = []byte(`{"A":"plain text","B":"escaped\ntext"}`)
	if err := Unmarshal(input, &value); err != nil {
		t.Fatal(err)
	}
	clear(input)
	if value.A != "plain text" || value.B != "escaped\ntext" {
		t.Fatalf("typed strings retained input storage: %#v", value)
	}
}

func TestDecodeGenericContainerConstruction(t *testing.T) {
	for _, count := range []int{0, 1, 7, 8, 9, 32, 129} {
		var input bytes.Buffer
		input.WriteString(`{"items":[`)
		for i := 0; i < count; i++ {
			if i != 0 {
				input.WriteByte(',')
			}
			fmt.Fprintf(&input, `{"x":[%d,"\n",[],{}],"dup":1,"dup":2}`, i)
		}
		input.WriteString(`],"last":null,"last":[],"empty":{}}`)
		checkDecodeAgainstStandard(t, input.String(), func() any { return nil })
	}
}

func TestDecodeWideFoldedFields(t *testing.T) {
	type fields struct {
		Lower int `json:"name"`
		Upper int `json:"NAME"`
		K     int `json:"K"`
		S     int `json:"S"`
		Sigma int `json:"Σ"`
		Final int `json:"ς"`
		Extra int `json:"extra"`
	}
	for _, input := range []string{
		`{"NAME":2,"NaMe":3,"name":4,"K":5,"ſ":6,"σ":7,"ς":8}`,
		`{"unknown":123,"Σ":1,"σ":2,"ς":3,"EXTRA":4}`,
		`{"na\u006de":5,"\u212a":6,"\u03c3":7}`,
	} {
		checkDecodeAgainstStandard(t, input, func() fields { return fields{} })
	}
}
