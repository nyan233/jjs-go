package compiler

import (
	"bytes"
	"encoding/json"
	"math"
	"reflect"
	"runtime"
	"testing"
	"unsafe"
)

func TestJITNilPointerAvoidsHelper(t *testing.T) {
	for _, tc := range pointerBenchmarkValues() {
		if tc.name != "NilRecursive" && tc.name != "NilLarge" && tc.name != "NilSlice128" {
			continue
		}
		t.Run(tc.name, func(t *testing.T) {
			p, err := compileJITEncoder(reflect.TypeOf(tc.value))
			if err != nil {
				t.Fatal(err)
			}
			for index := range p.program.helpers {
				if index != jitHelperReserve && index != jitHelperSlice {
					p.program.helpers[index] = reflect.ValueOf(unexpectedInlineHelper).Pointer()
				}
			}
			want, err := json.Marshal(tc.value)
			if err != nil {
				t.Fatal(err)
			}
			for _, capacity := range []int{0, 1, len(want) + 64} {
				got, err := p.encode(make([]byte, 0, capacity), reflect.ValueOf(tc.value).UnsafePointer())
				if err != nil || !bytes.Equal(got, want) {
					t.Fatalf("capacity %d: got %s err %v; want %s", capacity, got, err, want)
				}
			}
		})
	}
}

func TestJITPointerValues(t *testing.T) {
	for _, tc := range pointerBenchmarkValues() {
		t.Run(tc.name, func(t *testing.T) { checkInlineJSON(t, tc.value) })
	}
	// A finite recursive chain must grow the Go stack instead of expanding the
	// type forever or recursing through unchecked JIT-to-JIT calls.
	var chain *pointerBenchNode
	for i := 0; i < 1024; i++ {
		chain = &pointerBenchNode{i, chain}
	}
	checkInlineJSON(t, &pointerBenchWrapper{7, chain, 9})
	number := 42
	p1 := &number
	p2 := &p1
	p3 := &p2
	p4 := &p3
	p5 := &p4
	checkInlineJSON(t, &struct {
		Pointer ******int
		Nil     ******int
		After   int
	}{&p5, nil, 9})
	var dynamic any = map[string]int{"answer": 42}
	checkMapJSON(t, &struct {
		Pointer *any
		After   int
	}{&dynamic, 9})
	dynamic = (*pointerBenchNode)(nil)
	checkInlineJSON(t, &struct {
		Pointer *any
		After   int
	}{&dynamic, 9})
	// Pointer receivers remain observable through statically typed pointers.
	custom := inlineGCValue(7)
	checkInlineJSON(t, &struct {
		Pointer *inlineGCValue
		Nil     *inlineGCValue
	}{&custom, nil})
	type errorNode struct {
		Next  *errorNode
		Value float64
	}
	value := struct {
		Pointer *errorNode
		After   int
	}{&errorNode{Value: math.NaN()}, 9}
	checkInlineJSON(t, &value)
	value.Pointer.Value = 1.25
	checkInlineJSON(t, &value)
}

//go:noinline
func pointerTestSpill(ctx **jitEncodeContext, ptr *unsafe.Pointer, index *int) {
	runtime.KeepAlive(ctx)
	runtime.KeepAlive(ptr)
	runtime.KeepAlive(index)
}

//go:noinline
func pointerTestValueWithGC(ctx *jitEncodeContext, ptr unsafe.Pointer, index int) {
	pointerTestSpill(&ctx, &ptr, &index)
	frameTestGrow(32)
	jitEncodeValue(ctx, ptr, index)
}

func TestJITPointerOutlinedStackGrowth(t *testing.T) {
	for _, tc := range pointerBenchmarkValues() {
		if tc.name != "Large" && tc.name != "Recursive32" {
			continue
		}
		t.Run(tc.name, func(t *testing.T) {
			p, err := compileJITEncoder(reflect.TypeOf(tc.value))
			if err != nil {
				t.Fatal(err)
			}
			p.program.helpers[jitHelperValue] = reflect.ValueOf(pointerTestValueWithGC).Pointer()
			done := make(chan struct{})
			go func() {
				defer close(done)
				got, err := p.encode(nil, reflect.ValueOf(tc.value).UnsafePointer())
				want, wantErr := json.Marshal(tc.value)
				if err != nil || wantErr != nil || !bytes.Equal(got, want) {
					t.Errorf("got %.200s, err %v; want %.200s, err %v", got, err, want, wantErr)
				}
			}()
			<-done
		})
	}
}

func TestJITPointerAtInlineDepthLimit(t *testing.T) {
	type level4 map[string]*int
	type level3 map[string]level4
	type level2 map[string]level3
	type level1 map[string]level2
	number := 42
	value := level1{"outer": {"middle": {"inner": {"nil": nil, "number": &number}}}}
	checkMapJSON(t, value)
	p, err := compileJITEncoder(reflect.TypeOf(value))
	if err != nil {
		t.Fatal(err)
	}
	p.program.helpers[jitHelperValue] = reflect.ValueOf(pointerTestValueWithGC).Pointer()
	got, err := p.encode(nil, reflect.ValueOf(value).UnsafePointer())
	if err != nil {
		t.Fatal(err)
	}
	want, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decodeMapJSON(t, got), decodeMapJSON(t, want)) {
		t.Fatalf("got %s, want %s", got, want)
	}
}
