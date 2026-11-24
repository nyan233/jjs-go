package compiler

import (
	"reflect"
	"testing"
	"unsafe"
)

type TestJson1 struct {
	UserName     string
	UserBase64Id string
}

type TestJITNested struct {
	Name   string         `json:"name"`
	Child  TestJson1      `json:"child"`
	Values [2]int         `json:"values"`
	Items  []int          `json:"items"`
	Meta   map[string]int `json:"meta"`
}

type TestJITPointer struct {
	Name string     `json:"name"`
	Next *TestJson1 `json:"next"`
}

func TestEncodeCompile(t *testing.T) {
	j := &TestJson1{
		UserName:     "Hello-World",
		UserBase64Id: "Base64-Id",
	}
	handler, err := gEncoder.Compile(reflect.TypeOf(j), 0)
	if err != nil {
		t.Fatal(err)
	}
	var buf []byte
	if err := handler(&buf, unsafe.Pointer(j)); err != nil {
		t.Fatal(err)
	}
	const want = `{"UserName":"Hello-World","UserBase64Id":"Base64-Id"}`
	if string(buf) != want {
		t.Fatalf("got %s, want %s", buf, want)
	}
}

func TestEncodeJITNested(t *testing.T) {
	value := &TestJITNested{
		Name:   "root",
		Child:  TestJson1{UserName: "child", UserBase64Id: "id"},
		Values: [2]int{1, -2},
		Items:  []int{4, 5, 6},
		Meta:   map[string]int{"answer": 42},
	}
	handler, err := CompileEncoder(reflect.TypeOf(value), 0)
	if err != nil {
		t.Fatal(err)
	}
	var buf []byte
	if err := handler(&buf, unsafe.Pointer(value)); err != nil {
		t.Fatal(err)
	}
	const want = `{"name":"root","child":{"UserName":"child","UserBase64Id":"id"},"values":[1,-2],"items":[4,5,6],"meta":{"answer":42}}`
	if string(buf) != want {
		t.Fatalf("got %s, want %s", buf, want)
	}
}

func TestEncodeJITPointer(t *testing.T) {
	value := &TestJITPointer{
		Name: "root",
		Next: &TestJson1{UserName: "child", UserBase64Id: "id"},
	}
	handler, err := CompileEncoder(reflect.TypeOf(value), 0)
	if err != nil {
		t.Fatal(err)
	}
	var buf []byte
	if err := handler(&buf, unsafe.Pointer(value)); err != nil {
		t.Fatal(err)
	}
	const want = `{"name":"root","next":{"UserName":"child","UserBase64Id":"id"}}`
	if string(buf) != want {
		t.Fatalf("got %s, want %s", buf, want)
	}
}

func TestMapIteratorLayout(t *testing.T) {
	if !jitMapLayout.valid {
		t.Fatal("invalid map frame layout")
	}
	if !jitRawMapLayout.valid {
		t.Fatal("invalid reflect map iterator layout")
	}
	if jitMapLayout.name != int64(unsafe.Offsetof(jitMapFrame{}.name)) ||
		jitMapLayout.elem != int64(unsafe.Offsetof(jitMapFrame{}.elem)) {
		t.Fatalf("unexpected map frame layout: %+v", jitMapLayout)
	}
}

func TestMapIteratorRuntime(t *testing.T) {
	value := map[string]int{"b": 2, "a": 1}
	ctx := &jitEncodeContext{program: &jitEncodeProgram{mapTypes: []reflect.Type{reflect.TypeOf(value)}}}
	jitMapStart(ctx, unsafe.Pointer(&value), 0)
	frame := ctx.mapActive
	if frame == nil || frame.name != "a" || *(*int)(frame.elem) != 1 {
		t.Fatalf("first sorted entry = %+v", frame)
	}
	if !jitMapNext(ctx) || frame.name != "b" || *(*int)(frame.elem) != 2 {
		t.Fatalf("second sorted entry = %+v", frame)
	}
	if jitMapNext(ctx) {
		t.Fatal("unexpected additional entry")
	}
	jitMapEnd(ctx)
	if frame.name != "" || frame.elem != nil || len(frame.entries) != 0 || len(frame.names) != 0 {
		t.Fatal("mapEnd retained key or value")
	}
}

func TestTypePointerMatchesInterface(t *testing.T) {
	typ := reflect.TypeOf(TestJson1{})
	var value any = TestJson1{}
	actual := *(*unsafe.Pointer)(unsafe.Pointer(&value))
	if got := unsafe.Pointer(typePointer(typ)); got != actual {
		t.Fatalf("type pointer mismatch: registry=%p interface=%p", got, actual)
	}
}
