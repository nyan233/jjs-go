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
