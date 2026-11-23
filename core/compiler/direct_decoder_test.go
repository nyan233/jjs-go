package compiler

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSkipDirectValueValidation(t *testing.T) {
	for _, input := range []string{
		`"plain"`, `"中文"`, "\"bad\xffutf8\"", `"\uD800"`, `"\uDC00"`,
		`"\uD800\u0041"`, `"\uD83D\uDE00"`, `"\\\"\/\b\f\n\r\t"`,
		`{"escaped\u006bey":[null,true,false,-1.25e+2,{"x":"\n"}]}`,
		`"\u"`, `"\u12"`, `"\u12x4"`, `"\uD800\uZZZZ"`, `"\x"`,
		`"unterminated`, `"trailing\`, "\"control\x00\"", "\"line\n\"",
		`{"a":}`, `{"a" 1}`, `{"a":1,}`, `[1,]`, `[01]`, `[1e+]`,
		strings.Repeat(" ", 64) + `{"a":["\uD800", "\n"]}` + "\r\n\t",
	} {
		p := jitJSONParser{data: []byte(input)}
		err := p.skipDirectValue()
		p.skipSpace()
		valid := err == nil && p.index == len(p.data)
		if valid != json.Valid([]byte(input)) {
			t.Errorf("%q: valid=%t, err=%v", input, valid, err)
		}
		if p.depth != 0 {
			t.Errorf("%q: depth after return = %d", input, p.depth)
		}
	}
}

func TestSkipDirectValueDepthRestored(t *testing.T) {
	p := jitJSONParser{data: []byte(strings.Repeat("[", 1001) + "0" + strings.Repeat("]", 1001))}
	if err := p.skipDirectValue(); err == nil {
		t.Fatal("accepted excessive nesting")
	}
	if p.depth != 0 {
		t.Fatalf("depth after rejected nesting = %d, want 0", p.depth)
	}
}

func TestSkipDirectValueDoesNotAllocateStrings(t *testing.T) {
	data := []byte(`{"escaped\u006bey":["long enough to allocate\n中文\uD800",{"x":"\t"}]}`)
	allocs := testing.AllocsPerRun(100, func() {
		p := jitJSONParser{data: data}
		if err := p.skipDirectValue(); err != nil {
			t.Fatal(err)
		}
	})
	if allocs != 0 {
		t.Fatalf("skipping a value allocated %.0f objects", allocs)
	}
}

func FuzzSkipDirectValue(f *testing.F) {
	for _, input := range []string{`{"a":[1,"\n"]}`, `"\uD800"`, `"\uZZZZ"`, `"bad` + "\xff" + `"`} {
		f.Add(input)
	}
	f.Fuzz(func(t *testing.T, input string) {
		// Stay below the decoder's documented nesting limit.
		if len(input) > 900 {
			return
		}
		p := jitJSONParser{data: []byte(input)}
		err := p.skipDirectValue()
		p.skipSpace()
		if (err == nil && p.index == len(p.data)) != json.Valid([]byte(input)) {
			t.Fatalf("%q: err=%v, index=%d", input, err, p.index)
		}
	})
}
