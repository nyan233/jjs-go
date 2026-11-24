package compiler

import (
	"encoding"
	stdjson "encoding/json"
	"reflect"
	"strconv"
	"unsafe"
)

var (
	jitTextMarshalerType   = reflect.TypeOf((*encoding.TextMarshaler)(nil)).Elem()
	jitTextUnmarshalerType = reflect.TypeOf((*encoding.TextUnmarshaler)(nil)).Elem()
)

func supportsJITMapKeyEncode(typ reflect.Type) bool {
	switch typ.Kind() {
	case reflect.String,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return true
	default:
		return typ.Implements(jitTextMarshalerType)
	}
}

func supportsJITMapKeyDecode(typ reflect.Type) bool {
	switch typ.Kind() {
	case reflect.String,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return true
	default:
		return reflect.PointerTo(typ).Implements(jitTextUnmarshalerType)
	}
}

// assignDecodedMapKey follows encoding/json's map-key conversion order:
// TextUnmarshaler first, then string and integer kinds. The key storage only
// needs to live until mapassign has copied it into the map.
func assignDecodedMapKey(mapType, hmap unsafe.Pointer, keyType reflect.Type, textUnmarshaler bool, text string) (unsafe.Pointer, error) {
	if textUnmarshaler {
		key := reflect.New(keyType)
		unmarshaler := key.Interface().(encoding.TextUnmarshaler)
		if err := unmarshaler.UnmarshalText([]byte(text)); err != nil {
			return nil, err
		}
		return runtimeMapAssign(mapType, hmap, key.UnsafePointer()), nil
	}

	switch keyType.Kind() {
	case reflect.String:
		key := text
		return runtimeMapAssign(mapType, hmap, unsafe.Pointer(&key)), nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		value, err := strconv.ParseInt(text, 10, 64)
		if err != nil || reflect.Zero(keyType).OverflowInt(value) {
			return nil, &stdjson.UnmarshalTypeError{Value: "number " + text, Type: keyType}
		}
		var key [8]byte
		switch keyType.Bits() {
		case 8:
			*(*int8)(unsafe.Pointer(&key[0])) = int8(value)
		case 16:
			*(*int16)(unsafe.Pointer(&key[0])) = int16(value)
		case 32:
			*(*int32)(unsafe.Pointer(&key[0])) = int32(value)
		case 64:
			*(*int64)(unsafe.Pointer(&key[0])) = value
		default:
			panic("json: unexpected signed map-key width")
		}
		return runtimeMapAssign(mapType, hmap, unsafe.Pointer(&key[0])), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		value, err := strconv.ParseUint(text, 10, 64)
		if err != nil || reflect.Zero(keyType).OverflowUint(value) {
			return nil, &stdjson.UnmarshalTypeError{Value: "number " + text, Type: keyType}
		}
		var key [8]byte
		switch keyType.Bits() {
		case 8:
			*(*uint8)(unsafe.Pointer(&key[0])) = uint8(value)
		case 16:
			*(*uint16)(unsafe.Pointer(&key[0])) = uint16(value)
		case 32:
			*(*uint32)(unsafe.Pointer(&key[0])) = uint32(value)
		case 64:
			*(*uint64)(unsafe.Pointer(&key[0])) = value
		default:
			panic("json: unexpected unsigned map-key width")
		}
		return runtimeMapAssign(mapType, hmap, unsafe.Pointer(&key[0])), nil
	default:
		return nil, &stdjson.UnsupportedTypeError{Type: keyType}
	}
}
