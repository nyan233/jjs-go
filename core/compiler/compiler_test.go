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
		t.Fatal("unsupported reflect.MapIter pointer fields")
	}
	if jitMapLayout.elem != jitMapLayout.key+int64(unsafe.Sizeof(unsafe.Pointer(nil))) {
		t.Fatalf("unexpected iterator layout: %+v", jitMapLayout)
	}
}

func TestMapIteratorRuntime(t *testing.T) {
	value := map[string]int{"answer": 42}
	frame := new(jitMapFrame)
	frame.iterator.Reset(reflect.ValueOf(value))
	if !frame.iterator.Next() {
		t.Fatal("missing map entry")
	}
	key := *(*unsafe.Pointer)(unsafe.Add(unsafe.Pointer(frame), jitMapLayout.key))
	elem := *(*unsafe.Pointer)(unsafe.Add(unsafe.Pointer(frame), jitMapLayout.elem))
	if key == nil || elem == nil {
		t.Fatal("runtime iterator returned nil key or elem")
	}
	if got := *(*string)(key); got != "answer" {
		t.Fatalf("key = %q", got)
	}
	if got := *(*int)(elem); got != 42 {
		t.Fatalf("element = %d", got)
	}
	if frame.iterator.Next() {
		t.Fatal("unexpected additional entry")
	}
	frame.iterator.Reset(reflect.Value{})
	if *(*unsafe.Pointer)(unsafe.Add(unsafe.Pointer(frame), jitMapLayout.key)) != nil ||
		*(*unsafe.Pointer)(unsafe.Add(unsafe.Pointer(frame), jitMapLayout.elem)) != nil {
		t.Fatal("Reset retained key/element")
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
