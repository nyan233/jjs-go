package jjs_go

import (
	"encoding/json"
	"reflect"

	"github.com/nyan233/jjs-go/core/compiler"
)

// Marshal returns the JSON encoding of data.
func Marshal(data any) ([]byte, error) {
	value := reflect.ValueOf(data)
	if !value.IsValid() {
		return []byte("null"), nil
	}
	encoder, err := compiler.CompileEncoder(value.Type(), 0)
	if err != nil {
		return nil, err
	}
	buf := make([]byte, 0, 256)
	if err := encoder(&buf, value); err != nil {
		return nil, err
	}
	return buf, nil
}

// Unmarshal parses JSON data and stores the result in the value pointed to by
// data. data must be a non-nil pointer.
func Unmarshal(bytes []byte, data any) error {
	target := reflect.ValueOf(data)
	if !target.IsValid() || target.Kind() != reflect.Pointer || target.IsNil() {
		return &json.InvalidUnmarshalError{Type: reflect.TypeOf(data)}
	}
	decoder, err := compiler.CompileDecoder(target.Elem().Type())
	if err != nil {
		return err
	}
	return decoder(bytes, target.Elem())
}
