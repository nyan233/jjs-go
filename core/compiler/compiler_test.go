package compiler

import (
	"reflect"
	"testing"
)

type TestJson1 struct {
	UserName     string
	UserBase64Id string
}

func TestEncodeCompile(t *testing.T) {
	j := &TestJson1{
		UserName:     "Hello-World",
		UserBase64Id: "Base64-Id",
	}
	_, err := gEncoder.Compile(reflect.TypeOf(j), 0)
	if err != nil {
		t.Fatal(err)
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
