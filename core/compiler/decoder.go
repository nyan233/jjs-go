package compiler

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sync"
)

var gDecoder = new(Decoder).init()

// CompileDecoder compiles and caches a decoder for typ.
//
// Decoding deliberately uses encoding/json below the type-specialized cache.
// JSON decoding has a large correctness surface (custom unmarshalers, tags,
// embedded fields, numbers, strings and error positions) and must not depend on
// an incompletely verified JIT path.
func CompileDecoder(typ reflect.Type) (DecodeHandler, error) {
	return gDecoder.Compile(typ)
}

type Decoder struct {
	baseCache[DecodeHandler]
	mu sync.Mutex
}

func (d *Decoder) init() *Decoder {
	d.baseCache.init()
	return d
}

func (d *Decoder) Compile(typ reflect.Type) (DecodeHandler, error) {
	if typ == nil {
		return nil, errors.New("json: cannot compile a nil type")
	}
	if handler, ok := d.getCompileResult(typ); ok {
		return handler, nil
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	if handler, ok := d.getCompileResult(typ); ok {
		return handler, nil
	}

	handler := func(data []byte, val reflect.Value) error {
		if !val.IsValid() {
			return errors.New("json: invalid decode destination")
		}
		if val.Type() != typ {
			return fmt.Errorf("json: decoder compiled for %s, got %s", typ, val.Type())
		}
		if val.CanAddr() {
			return json.Unmarshal(data, val.Addr().Interface())
		}
		target := reflect.New(typ)
		if err := json.Unmarshal(data, target.Interface()); err != nil {
			return err
		}
		if val.CanSet() {
			val.Set(target.Elem())
			return nil
		}
		return errors.New("json: decode destination is not settable")
	}
	d.setCompileResult(typ, handler)
	return handler, nil
}
