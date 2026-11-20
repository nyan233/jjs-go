package compiler

import (
	"encoding"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"sort"
	"strconv"
	"sync"
	"unicode/utf8"
)

var gEncoder = new(Encoder).init()

var (
	marshalerType     = reflect.TypeOf((*json.Marshaler)(nil)).Elem()
	textMarshalerType = reflect.TypeOf((*encoding.TextMarshaler)(nil)).Elem()
	jsonNumberType    = reflect.TypeOf(json.Number(""))
)

// CompileEncoder compiles and caches an encoding plan for typ.
func CompileEncoder(typ reflect.Type, opt uint64) (EncodeHandler, error) {
	return gEncoder.Compile(typ, opt)
}

type Encoder struct {
	baseCache[EncodeHandler]
	mu sync.Mutex
}

func (e *Encoder) init() *Encoder {
	e.baseCache.init()
	return e
}

func (e *Encoder) Compile(typ reflect.Type, _ uint64) (EncodeHandler, error) {
	if typ == nil {
		return nil, errors.New("json: cannot compile a nil type")
	}
	if handler, ok := e.getCompileResult(typ); ok {
		return handler, nil
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	if handler, ok := e.getCompileResult(typ); ok {
		return handler, nil
	}

	plan, err := buildEncodePlan(typ, make(map[reflect.Type]*encodePlan))
	if err != nil {
		return nil, err
	}
	handler := func(buf *[]byte, val reflect.Value) error {
		if buf == nil {
			return errors.New("json: nil destination buffer")
		}
		if !val.IsValid() {
			*buf = append((*buf)[:0], "null"...)
			return nil
		}
		if val.Type() != typ {
			return fmt.Errorf("json: encoder compiled for %s, got %s", typ, val.Type())
		}
		state := encodeState{
			dst: (*buf)[:0],
		}
		if err := state.encode(plan, val); err != nil {
			*buf = state.dst
			return err
		}
		*buf = state.dst
		return nil
	}
	e.setCompileResult(typ, handler)
	return handler, nil
}

type encodeKind uint8

const (
	encodeFallback encodeKind = iota
	encodeBool
	encodeInt
	encodeUint
	encodeFloat
	encodeJSONNumber
	encodeString
	encodeBytes
	encodePointer
	encodeStruct
	encodeSlice
	encodeArray
	encodeMap
)

type encodePlan struct {
	typ    reflect.Type
	kind   encodeKind
	elem   *encodePlan
	fields []encodeField
}

type encodeField struct {
	index     int
	name      []byte
	omitEmpty bool
	plan      *encodePlan
}

func buildEncodePlan(typ reflect.Type, cache map[reflect.Type]*encodePlan) (*encodePlan, error) {
	if typ == nil {
		return nil, errors.New("json: nil type in encoding plan")
	}
	if plan := cache[typ]; plan != nil {
		return plan, nil
	}

	plan := &encodePlan{typ: typ}
	cache[typ] = plan

	if typ == jsonNumberType {
		plan.kind = encodeJSONNumber
		return plan, nil
	}
	if implementsCustomMarshaler(typ) {
		plan.kind = encodeFallback
		return plan, nil
	}

	switch typ.Kind() {
	case reflect.Bool:
		plan.kind = encodeBool
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		plan.kind = encodeInt
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		plan.kind = encodeUint
	case reflect.Float32, reflect.Float64:
		plan.kind = encodeFloat
	case reflect.String:
		plan.kind = encodeString
	case reflect.Pointer:
		plan.kind = encodePointer
		elem, err := buildEncodePlan(typ.Elem(), cache)
		if err != nil {
			return nil, err
		}
		plan.elem = elem
	case reflect.Struct:
		plan.kind = encodeStruct
		fieldNames := make(map[string]struct{}, typ.NumField())
		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)
			if field.Anonymous {
				return fallbackPlan(plan), nil
			}
			if field.PkgPath != "" {
				continue
			}
			name, omitEmpty, unsupported := encodeFieldName(field)
			if unsupported {
				return fallbackPlan(plan), nil
			}
			if name == "" {
				continue
			}
			if _, duplicate := fieldNames[name]; duplicate {
				return fallbackPlan(plan), nil
			}
			fieldNames[name] = struct{}{}
			fieldPlan, err := buildEncodePlan(field.Type, cache)
			if err != nil {
				return nil, err
			}
			plan.fields = append(plan.fields, encodeField{
				index:     i,
				name:      append(appendQuoted(nil, name), ':'),
				omitEmpty: omitEmpty,
				plan:      fieldPlan,
			})
		}
	case reflect.Slice:
		if typ.Elem().Kind() == reflect.Uint8 {
			plan.kind = encodeBytes
			return plan, nil
		}
		plan.kind = encodeSlice
		elem, err := buildEncodePlan(typ.Elem(), cache)
		if err != nil {
			return nil, err
		}
		plan.elem = elem
	case reflect.Array:
		plan.kind = encodeArray
		elem, err := buildEncodePlan(typ.Elem(), cache)
		if err != nil {
			return nil, err
		}
		plan.elem = elem
	case reflect.Map:
		if typ.Key().Kind() != reflect.String || implementsCustomMarshaler(typ.Key()) {
			return fallbackPlan(plan), nil
		}
		plan.kind = encodeMap
		elem, err := buildEncodePlan(typ.Elem(), cache)
		if err != nil {
			return nil, err
		}
		plan.elem = elem
	default:
		// Interfaces, functions, channels and custom types retain
		// encoding/json semantics through a safe fallback.
		plan.kind = encodeFallback
	}
	return plan, nil
}

func fallbackPlan(plan *encodePlan) *encodePlan {
	plan.kind = encodeFallback
	plan.fields = nil
	plan.elem = nil
	return plan
}

func implementsCustomMarshaler(typ reflect.Type) bool {
	if typ.Implements(marshalerType) || typ.Implements(textMarshalerType) {
		return true
	}
	return typ.Kind() != reflect.Pointer &&
		(reflect.PointerTo(typ).Implements(marshalerType) || reflect.PointerTo(typ).Implements(textMarshalerType))
}

func encodeFieldName(field reflect.StructField) (name string, omitEmpty, unsupported bool) {
	tag := field.Tag.Get("json")
	if tag == "-" {
		return "", false, false
	}
	name, options := splitJSONTag(tag)
	if name == "" {
		name = field.Name
	}
	for options != "" {
		option, rest, found := cutString(options, ",")
		switch option {
		case "omitempty":
			omitEmpty = true
		case "":
		case "string":
			// ,string changes scalar semantics; use encoding/json for
			// the complete struct to avoid a partial implementation.
			unsupported = true
		default:
			unsupported = true
		}
		options = rest
		if !found {
			break
		}
	}
	if !isValidJSONTagName(name) {
		name = field.Name
	}
	return name, omitEmpty, unsupported
}

func splitJSONTag(tag string) (name, options string) {
	if tag == "" {
		return "", ""
	}
	name, options, _ = cutString(tag, ",")
	return
}

func cutString(s, sep string) (before, after string, found bool) {
	for i := 0; i+len(sep) <= len(s); i++ {
		if s[i:i+len(sep)] == sep {
			return s[:i], s[i+len(sep):], true
		}
	}
	return s, "", false
}

func isValidJSONTagName(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		if r == ',' || r == '\\' || r == '"' || r <= ' ' || r == utf8.RuneError {
			return false
		}
	}
	return true
}

type visitKey struct {
	typ reflect.Type
	ptr uintptr
}

type encodeState struct {
	dst     []byte
	seen    [32]visitKey
	visited int
}

func (s *encodeState) encode(plan *encodePlan, val reflect.Value) error {
	if !val.IsValid() {
		s.dst = append(s.dst, "null"...)
		return nil
	}
	if val.Kind() == reflect.Interface && !val.IsNil() {
		val = val.Elem()
	}
	if val.Kind() == reflect.Pointer && val.IsNil() {
		s.dst = append(s.dst, "null"...)
		return nil
	}

	switch plan.kind {
	case encodeFallback:
		return s.appendJSON(val)
	case encodeBool:
		s.dst = strconv.AppendBool(s.dst, val.Bool())
	case encodeInt:
		s.dst = strconv.AppendInt(s.dst, val.Int(), 10)
	case encodeUint:
		s.dst = strconv.AppendUint(s.dst, val.Uint(), 10)
	case encodeFloat:
		return s.appendFloat(val)
	case encodeJSONNumber:
		number := val.String()
		if number == "" {
			number = "0"
		}
		if (number[0] != '-' && (number[0] < '0' || number[0] > '9')) || !json.Valid([]byte(number)) {
			return fmt.Errorf("json: invalid number literal %q", number)
		}
		s.dst = append(s.dst, number...)
	case encodeString:
		s.dst = appendQuoted(s.dst, val.String())
	case encodeBytes:
		if val.IsNil() {
			s.dst = append(s.dst, "null"...)
			return nil
		}
		encoded := base64.StdEncoding.EncodeToString(val.Bytes())
		s.dst = append(s.dst, '"')
		s.dst = append(s.dst, encoded...)
		s.dst = append(s.dst, '"')
	case encodePointer:
		key := visitKey{typ: val.Type(), ptr: val.Pointer()}
		if err := s.enter(key); err != nil {
			return err
		}
		err := s.encode(plan.elem, val.Elem())
		s.leave()
		return err
	case encodeStruct:
		return s.encodeStruct(plan, val)
	case encodeSlice:
		return s.encodeSlice(plan, val)
	case encodeArray:
		return s.encodeArray(plan, val)
	case encodeMap:
		return s.encodeMap(plan, val)
	default:
		return fmt.Errorf("json: unknown encoding plan kind %d", plan.kind)
	}
	return nil
}

func (s *encodeState) enter(key visitKey) error {
	for i := 0; i < s.visited; i++ {
		if s.seen[i] == key {
			return errors.New("json: unsupported value: encountered a cycle")
		}
	}
	if s.visited == len(s.seen) {
		return errors.New("json: maximum JSON encoding depth exceeded")
	}
	s.seen[s.visited] = key
	s.visited++
	return nil
}

func (s *encodeState) leave() {
	s.visited--
}

func (s *encodeState) encodeStruct(plan *encodePlan, val reflect.Value) error {
	s.dst = append(s.dst, '{')
	written := 0
	for i := range plan.fields {
		field := &plan.fields[i]
		fieldValue := val.Field(field.index)
		if field.omitEmpty && isEmptyValue(fieldValue) {
			continue
		}
		if written > 0 {
			s.dst = append(s.dst, ',')
		}
		s.dst = append(s.dst, field.name...)
		if err := s.encode(field.plan, fieldValue); err != nil {
			return err
		}
		written++
	}
	s.dst = append(s.dst, '}')
	return nil
}

func (s *encodeState) encodeSlice(plan *encodePlan, val reflect.Value) error {
	if val.IsNil() {
		s.dst = append(s.dst, "null"...)
		return nil
	}
	key := visitKey{typ: val.Type(), ptr: val.Pointer()}
	if err := s.enter(key); err != nil {
		return err
	}
	defer s.leave()

	s.dst = append(s.dst, '[')
	for i := 0; i < val.Len(); i++ {
		if i > 0 {
			s.dst = append(s.dst, ',')
		}
		if err := s.encode(plan.elem, val.Index(i)); err != nil {
			return err
		}
	}
	s.dst = append(s.dst, ']')
	return nil
}

func (s *encodeState) encodeArray(plan *encodePlan, val reflect.Value) error {
	s.dst = append(s.dst, '[')
	for i := 0; i < val.Len(); i++ {
		if i > 0 {
			s.dst = append(s.dst, ',')
		}
		if err := s.encode(plan.elem, val.Index(i)); err != nil {
			return err
		}
	}
	s.dst = append(s.dst, ']')
	return nil
}

func (s *encodeState) encodeMap(plan *encodePlan, val reflect.Value) error {
	if val.IsNil() {
		s.dst = append(s.dst, "null"...)
		return nil
	}
	s.dst = append(s.dst, '{')
	keys := make([]string, 0, val.Len())
	iter := val.MapRange()
	for iter.Next() {
		keys = append(keys, iter.Key().String())
	}
	sort.Strings(keys)
	written := 0
	for _, key := range keys {
		if written > 0 {
			s.dst = append(s.dst, ',')
		}
		s.dst = appendQuoted(s.dst, key)
		s.dst = append(s.dst, ':')
		if err := s.encode(plan.elem, val.MapIndex(reflect.ValueOf(key).Convert(plan.typ.Key()))); err != nil {
			return err
		}
		written++
	}
	s.dst = append(s.dst, '}')
	return nil
}

func (s *encodeState) appendFloat(val reflect.Value) error {
	number := val.Float()
	if math.IsNaN(number) || math.IsInf(number, 0) {
		return &json.UnsupportedValueError{
			Value: val,
			Str:   strconv.FormatFloat(number, 'g', -1, val.Type().Bits()),
		}
	}

	format := byte('f')
	abs := math.Abs(number)
	if abs != 0 && (abs < 1e-6 || abs >= 1e21) {
		format = 'e'
	}
	bits := val.Type().Bits()
	s.dst = strconv.AppendFloat(s.dst, number, format, -1, bits)
	if format == 'e' {
		// Match encoding/json by removing a leading zero from a two-digit
		// negative exponent (1e-09 -> 1e-9).
		if n := len(s.dst); n >= 4 && s.dst[n-4] == 'e' && s.dst[n-3] == '-' && s.dst[n-2] == '0' {
			s.dst[n-2] = s.dst[n-1]
			s.dst = s.dst[:n-1]
		}
	}
	return nil
}

func (s *encodeState) appendJSON(val reflect.Value) error {
	if val.Kind() == reflect.Pointer && val.IsNil() {
		s.dst = append(s.dst, "null"...)
		return nil
	}
	target := val.Interface()
	if val.Kind() != reflect.Pointer && val.CanAddr() {
		target = val.Addr().Interface()
	}
	raw, err := json.Marshal(target)
	if err != nil {
		return err
	}
	s.dst = append(s.dst, raw...)
	return nil
}

func isEmptyValue(val reflect.Value) bool {
	switch val.Kind() {
	case reflect.Array, reflect.Map, reflect.Slice, reflect.String:
		return val.Len() == 0
	case reflect.Bool:
		return !val.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return val.Int() == 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return val.Uint() == 0
	case reflect.Float32, reflect.Float64:
		return val.Float() == 0
	case reflect.Interface, reflect.Pointer:
		return val.IsNil()
	}
	return false
}

func appendQuoted(dst []byte, value string) []byte {
	const hex = "0123456789abcdef"
	dst = append(dst, '"')
	for i := 0; i < len(value); {
		c := value[i]
		if c < utf8.RuneSelf {
			switch c {
			case '\\', '"':
				dst = append(dst, '\\', c)
			case '\b':
				dst = append(dst, '\\', 'b')
			case '\f':
				dst = append(dst, '\\', 'f')
			case '\n':
				dst = append(dst, '\\', 'n')
			case '\r':
				dst = append(dst, '\\', 'r')
			case '\t':
				dst = append(dst, '\\', 't')
			case '<':
				dst = append(dst, '\\', 'u', '0', '0', '3', 'c')
			case '>':
				dst = append(dst, '\\', 'u', '0', '0', '3', 'e')
			case '&':
				dst = append(dst, '\\', 'u', '0', '0', '2', '6')
			default:
				if c < 0x20 {
					dst = append(dst, '\\', 'u', '0', '0', hex[c>>4], hex[c&0x0f])
				} else {
					dst = append(dst, c)
				}
			}
			i++
			continue
		}

		r, size := utf8.DecodeRuneInString(value[i:])
		if r == utf8.RuneError && size == 1 {
			dst = append(dst, '\\', 'u', 'f', 'f', 'f', 'd')
			i++
			continue
		}
		if r == '\u2028' || r == '\u2029' {
			dst = append(dst, '\\', 'u', '2', '0', '2', byte('8'+r-'\u2028'))
		} else {
			dst = append(dst, value[i:i+size]...)
		}
		i += size
	}
	return append(dst, '"')
}
