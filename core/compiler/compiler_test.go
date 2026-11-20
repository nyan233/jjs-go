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
	Name   string    `json:"name"`
	Child  TestJson1 `json:"child"`
	Values [2]int    `json:"values"`
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
	}
	handler, err := CompileEncoder(reflect.TypeOf(value), 0)
	if err != nil {
		t.Fatal(err)
	}
	var buf []byte
	if err := handler(&buf, unsafe.Pointer(value)); err != nil {
		t.Fatal(err)
	}
	const want = `{"name":"root","child":{"UserName":"child","UserBase64Id":"id"},"values":[1,-2]}`
	if string(buf) != want {
		t.Fatalf("got %s, want %s", buf, want)
	}
}

func TestParseUList(t *testing.T) {
	got := ParseUList[uint16](0x1122334455667788)
	want := []uint16{0x1122, 0x3344, 0x5566, 0x7788}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}

	bytes := ParseUList[byte](0x1122334455667788)
	wantBytes := []byte{0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88}
	if !reflect.DeepEqual(bytes, wantBytes) {
		t.Fatalf("got %#v, want %#v", bytes, wantBytes)
	}
}
