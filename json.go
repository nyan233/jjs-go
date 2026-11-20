package jjs_go

import (
	"encoding/json"
	"reflect"
	"unsafe"

	"github.com/nyan233/jjs-go/core/compiler"
	"github.com/nyan233/jjs-go/internal/rtype"
)

// Marshal returns the JSON encoding of data.
func Marshal(data any) ([]byte, error) {
	typ := reflect.TypeOf(data)
	if typ == nil {
		return []byte("null"), nil
	}
	face := (*rtype.EmptyFace)(unsafe.Pointer(&data))
	encoder, err := compiler.CompileEncoder(typ, 0)
	if err != nil {
		return nil, err
	}
	buf := make([]byte, 0, 256)
	if err := encoder(&buf, face.Val); err != nil {
		return nil, err
	}
	return buf, nil
}

// Unmarshal parses JSON data and stores the result in the value pointed to by
// data. data must be a non-nil pointer.
func Unmarshal(bytes []byte, data any) error {
	typ := reflect.TypeOf(data)
	if typ == nil || typ.Kind() != reflect.Pointer {
		return &json.InvalidUnmarshalError{Type: reflect.TypeOf(data)}
	}
	face := (*rtype.EmptyFace)(unsafe.Pointer(&data))
	if face.Val == nil {
		return &json.InvalidUnmarshalError{Type: typ}
	}
	decoder, err := compiler.CompileDecoder(typ.Elem())
	if err != nil {
		return err
	}
	return decoder(bytes, face.Val)
}
