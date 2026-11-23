package compiler

import (
	"bytes"
	"encoding/json"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"unsafe"
)

//go:noinline
func quoteTestSpill(dst **byte, capacity *int, src **byte, length *int) {
	runtime.KeepAlive(dst)
	runtime.KeepAlive(capacity)
	runtime.KeepAlive(src)
	runtime.KeepAlive(length)
}

//go:noinline
func quoteTestGrow(dst *byte, capacity int, src *byte, length int) (int, int) {
	quoteTestSpill(&dst, &capacity, &src, &length)
	frameTestGrow(32)
	return quoteJSONString(dst, capacity, src, length)
}

func TestARM64JITQuoteStackGrowth(t *testing.T) {
	value := struct {
		Map  map[string]string
		Tail string
	}{map[string]string{"中\n\xff": "\\\"<>&\u2028\u2029"}, strings.Repeat("escaped\x00中", 100)}
	program, err := compileJITEncoder(reflect.TypeOf(&value))
	if err != nil {
		t.Fatal(err)
	}
	program.program.helpers[jitHelperQuote] = reflect.ValueOf(quoteTestGrow).Pointer()
	program.program.helpers[jitHelperString] = reflect.ValueOf(unexpectedInlineHelper).Pointer()
	done := make(chan struct{})
	go func() {
		defer close(done)
		got, err := program.encode(nil, unsafe.Pointer(&value))
		if err != nil {
			t.Error(err)
			return
		}
		want, err := json.Marshal(value)
		if err != nil {
			t.Error(err)
			return
		}
		if !reflect.DeepEqual(decodeMapJSON(t, got), decodeMapJSON(t, want)) {
			t.Errorf("incorrect result after stack growth: %.100s", got)
		}
	}()
	<-done
	checkMapJSON(t, &value)
}

func TestARM64JITQuoteBoundaries(t *testing.T) {
	program, err := compileJITEncoder(reflect.TypeOf(""))
	if err != nil {
		t.Fatal(err)
	}
	program.program.helpers[jitHelperString] = reflect.ValueOf(unexpectedInlineHelper).Pointer()
	for _, size := range []int{0, 1, 7, 8, 15, 16, 17, 31, 32, 33, quoteChunkBytes - 1, quoteChunkBytes, quoteChunkBytes + 1} {
		for _, pattern := range []string{"a", "中😀é", "\x00\n\"\\<>&\u2028", "\xff\xed\xa0\x80"} {
			value := strings.Repeat(pattern, size/len(pattern)+1)[:size]
			want := appendQuotedGo(nil, value)
			for _, capacity := range []int{0, 1, len(value) + 2, len(want) + 7} {
				got, err := program.encode(make([]byte, 0, capacity), unsafe.Pointer(&value))
				if err != nil || !bytes.Equal(got, want) {
					t.Fatalf("size=%d capacity=%d err=%v got=%.100q want=%.100q", size, capacity, err, got, want)
				}
			}
		}
	}
}

func TestARM64SSAQuoteHelperArguments(t *testing.T) {
	if reflect.TypeOf(quoteJSONString).NumIn() != armEncoderArgs[jitHelperQuote] || reflect.TypeOf(jitQuoteTail).NumIn() != armEncoderArgs[jitHelperQuoteTail] {
		t.Fatal("quote helper SSA ABI metadata is stale")
	}
}
