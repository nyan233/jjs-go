package compiler

import (
	"reflect"
	"sync"
	"unsafe"
)

type decodeEntry struct {
	key   string
	value any
}

// Scratch containers are reusable; published strings and interface payloads
// are append-only. A later decode must never overwrite a returned value.
type decodeStorage struct {
	strings []byte
	floats  []float64
	boxes   []string
	values  []any
	entries []decodeEntry
}

var decodeStoragePool = sync.Pool{New: func() any { return new(decodeStorage) }}

var (
	decodeStringType = runtimeTypePointer(reflect.TypeOf(""))
	decodeFloatType  = runtimeTypePointer(reflect.TypeOf(float64(0)))
)

func (s *decodeStorage) release() {
	// String boxes hold pointers into published chunks. Do not keep old
	// results alive just because this scratch object remains in the pool.
	s.boxes = nil
	clear(s.values)
	clear(s.entries)
	if cap(s.values) > 4096 {
		s.values = nil
	}
	if cap(s.entries) > 4096 {
		s.entries = nil
	}
	s.values = s.values[:0]
	s.entries = s.entries[:0]
	decodeStoragePool.Put(s)
}

func (p *jitJSONParser) copyString(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	if p.storage == nil || len(raw) > 512 {
		return string(raw)
	}
	s := p.storage
	if cap(s.strings)-len(s.strings) < len(raw) {
		size := max(len(raw), min(max(2*cap(s.strings), 64), 16<<10))
		s.strings = make([]byte, 0, size)
	}
	start := len(s.strings)
	s.strings = append(s.strings, raw...)
	return unsafe.String(&s.strings[start], len(raw))
}

func (p *jitJSONParser) boxString(value string) any {
	if p.storage == nil || len(value) == 0 {
		return value
	}
	s := p.storage
	if len(s.boxes) == cap(s.boxes) {
		s.boxes = make([]string, 0, 32)
	}
	s.boxes = append(s.boxes, value)
	return storedInterface(decodeStringType, unsafe.Pointer(&s.boxes[len(s.boxes)-1]))
}

func (p *jitJSONParser) boxFloat(value float64) any {
	if p.storage == nil {
		return value
	}
	s := p.storage
	if len(s.floats) == cap(s.floats) {
		s.floats = make([]float64, 0, 32)
	}
	s.floats = append(s.floats, value)
	return storedInterface(decodeFloatType, unsafe.Pointer(&s.floats[len(s.floats)-1]))
}

func storedInterface(typ, data unsafe.Pointer) any {
	// Both pointers remain visible to the GC; data points into a typed slice.
	var value any
	header := (*nonEmptyInterface)(unsafe.Pointer(&value))
	header.tab, header.data = typ, data
	return value
}

var decodedEmptyArray any = []any{}

func (p *jitJSONParser) decodeAnyArray() (any, error) {
	p.index++
	p.skipSpace()
	if p.consumeByte(']') {
		return decodedEmptyArray, nil
	}
	if p.storage == nil {
		// Internal callers can also construct a parser without pooled scratch.
		p.storage = new(decodeStorage)
	}
	s := p.storage
	base := len(s.values)
	defer func() {
		clear(s.values[base:])
		s.values = s.values[:base]
	}()
	for {
		value, err := p.decodeDirectAny()
		if err != nil {
			return nil, err
		}
		s.values = append(s.values, value)
		p.skipSpace()
		if p.consumeByte(']') {
			values := make([]any, len(s.values)-base)
			copy(values, s.values[base:])
			return values, nil
		}
		if !p.consumeByte(',') {
			return nil, p.errorf("expected ',' or ']'")
		}
	}
}

func (p *jitJSONParser) decodeAnyObject() (any, error) {
	p.index++
	p.skipSpace()
	if p.consumeByte('}') {
		return map[string]any{}, nil
	}
	if p.storage == nil {
		p.storage = new(decodeStorage)
	}
	s := p.storage
	base := len(s.entries)
	defer func() {
		clear(s.entries[base:])
		s.entries = s.entries[:base]
	}()
	for {
		p.skipSpace()
		key, err := p.parseString()
		if err != nil {
			return nil, err
		}
		p.skipSpace()
		if !p.consumeByte(':') {
			return nil, p.errorf("expected ':'")
		}
		value, err := p.decodeDirectAny()
		if err != nil {
			return nil, err
		}
		s.entries = append(s.entries, decodeEntry{key: key, value: value})
		p.skipSpace()
		if p.consumeByte('}') {
			values := make(map[string]any, len(s.entries)-base)
			for _, entry := range s.entries[base:] {
				values[entry.key] = entry.value
			}
			return values, nil
		}
		if !p.consumeByte(',') {
			return nil, p.errorf("expected ',' or '}'")
		}
	}
}
