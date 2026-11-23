package jjs_go

import (
	"encoding/json"
	"reflect"
	"unsafe"

	"github.com/nyan233/jjs-go/core/compiler"
)

type emptyInterface struct {
	typ  unsafe.Pointer
	data unsafe.Pointer
}

// Marshal returns the JSON encoding of data.
func Marshal(data any) ([]byte, error) {
	return MarshalTo(nil, data)
}

// MarshalTo encodes data into dst[:0], reusing its backing storage where
// possible. The previous contents are replaced. Use the returned slice because
// encoding may grow the buffer. On error it returns nil and dst may be modified.
// The backing storage of dst must not overlap any storage reachable from data.
func MarshalTo(dst []byte, data any) ([]byte, error) {
	typ := reflect.TypeOf(data)
	if typ == nil {
		return append(dst[:0], "null"...), nil
	}
	face := (*emptyInterface)(unsafe.Pointer(&data))
	dataPointer := face.data
	if typ.Kind() != reflect.Pointer && typ.Kind() != reflect.Map && compiler.IsDirectInterfaceType(typ) {
		boxed := face.data
		dataPointer = unsafe.Pointer(&boxed)
	}
	dst, err := compiler.EncodeTo(typ, dst, dataPointer)
	if err != nil {
		return nil, err
	}
	return dst, nil
}

// Unmarshal parses JSON data and stores the result in the value pointed to by
// data. data must be a non-nil pointer.
func Unmarshal(bytes []byte, data any) error {
	typ := reflect.TypeOf(data)
	if typ == nil || typ.Kind() != reflect.Pointer {
		return &json.InvalidUnmarshalError{Type: reflect.TypeOf(data)}
	}
	if reflect.ValueOf(data).IsNil() {
		return &json.InvalidUnmarshalError{Type: typ}
	}
	decoder, err := compiler.CompileDecoder(typ.Elem())
	if err != nil {
		return err
	}
	return decoder(bytes, unsafe.Pointer(reflect.ValueOf(data).Pointer()))
}
