package compiler

import (
	stdjson "encoding/json"
	"errors"
	"fmt"
	"reflect"
	"unsafe"
)

type jitCustomCallKind uint8

const (
	jitCustomNone jitCustomCallKind = iota
	jitCustomValue
	jitCustomAddress
	jitCustomPointer
	jitCustomInterface
)

type jitCustomTypeInfo struct {
	typePtr     uintptr
	itab        uintptr
	directValue bool
}

var (
	jitMarshalerType   = reflect.TypeOf((*stdjson.Marshaler)(nil)).Elem()
	jitUnmarshalerType = reflect.TypeOf((*stdjson.Unmarshaler)(nil)).Elem()
)

func jitMarshalerCall(typ reflect.Type, addressable bool) (jitCustomCallKind, reflect.Type) {
	if typ == nil {
		return jitCustomNone, nil
	}
	if typ.Kind() == reflect.Interface {
		if typ.Implements(jitMarshalerType) {
			return jitCustomInterface, typ
		}
		return jitCustomNone, nil
	}
	if typ.Implements(jitMarshalerType) {
		if typ.Kind() == reflect.Pointer {
			return jitCustomPointer, typ
		}
		return jitCustomValue, typ
	}
	if addressable && typ.Kind() != reflect.Pointer {
		pointer := reflect.PointerTo(typ)
		if pointer.Implements(jitMarshalerType) {
			return jitCustomAddress, pointer
		}
	}
	return jitCustomNone, nil
}

func jitUnmarshalerCall(typ reflect.Type) (jitCustomCallKind, reflect.Type) {
	if typ == nil {
		return jitCustomNone, nil
	}
	if typ.Kind() == reflect.Interface {
		if typ.Implements(jitUnmarshalerType) {
			return jitCustomInterface, typ
		}
		return jitCustomNone, nil
	}
	if typ.Implements(jitUnmarshalerType) {
		if typ.Kind() == reflect.Pointer {
			return jitCustomPointer, typ
		}
		return jitCustomValue, typ
	}
	if typ.Kind() != reflect.Pointer {
		pointer := reflect.PointerTo(typ)
		if pointer.Implements(jitUnmarshalerType) {
			return jitCustomAddress, pointer
		}
	}
	return jitCustomNone, nil
}

func (c *jitEncoderCompiler) customTypeIndex(typ reflect.Type) int {
	c.customTypes = append(c.customTypes, makeJITMarshalerTypeInfo(typ))
	return len(c.customTypes) - 1
}

func makeJITMarshalerTypeInfo(typ reflect.Type) jitCustomTypeInfo {
	info := jitCustomTypeInfo{typePtr: typePointer(typ), directValue: IsDirectInterfaceType(typ)}
	if typ.Kind() == reflect.Interface {
		return info
	}
	value := reflect.Zero(typ).Interface()
	marshaler, ok := value.(stdjson.Marshaler)
	if !ok {
		return info
	}
	face := (*nonEmptyInterface)(unsafe.Pointer(&marshaler))
	info.itab = uintptr(face.tab)
	return info
}

func makeJITUnmarshalerTypeInfo(typ reflect.Type) jitCustomTypeInfo {
	info := jitCustomTypeInfo{typePtr: typePointer(typ)}
	if typ.Kind() == reflect.Interface {
		return info
	}
	value := reflect.Zero(typ).Interface()
	unmarshaler, ok := value.(stdjson.Unmarshaler)
	if !ok {
		return info
	}
	face := (*nonEmptyInterface)(unsafe.Pointer(&unmarshaler))
	info.itab = uintptr(face.tab)
	return info
}

func jitEncodeCustomMarshaler(ctx *jitEncodeContext, fieldPtr unsafe.Pointer, callKind, typeIndex int) {
	if ctx == nil || ctx.program == nil || typeIndex < 0 || typeIndex >= len(ctx.program.customTypes) {
		return
	}
	var (
		info    = ctx.program.customTypes[typeIndex]
		typePtr = unsafe.Pointer(info.typePtr)
		tab     = unsafe.Pointer(info.itab)
		data    = fieldPtr
		dynamic bool
	)
	switch jitCustomCallKind(callKind) {
	case jitCustomPointer:
		data = *(*unsafe.Pointer)(fieldPtr)
		if data == nil {
			jitAppendNull(ctx)
			return
		}
	case jitCustomValue, jitCustomAddress:
	case jitCustomInterface:
		dynamic = true
		itab := *(*unsafe.Pointer)(fieldPtr)
		if itab == nil {
			jitAppendNull(ctx)
			return
		}
		typePtr = *(*unsafe.Pointer)(unsafe.Add(itab, 8))
		data = *(*unsafe.Pointer)(unsafe.Add(fieldPtr, 8))
		if data == nil {
			jitAppendNull(ctx)
			return
		}
	default:
		ctx.setError(errors.New("json: invalid custom marshaler mode"))
		return
	}

	var marshaler stdjson.Marshaler
	var dynamicValue any
	if dynamic {
		face := (*nonEmptyInterface)(unsafe.Pointer(&dynamicValue))
		face.tab = typePtr
		face.data = data
		var ok bool
		marshaler, ok = dynamicValue.(stdjson.Marshaler)
		if !ok {
			ctx.setError(fmt.Errorf("json: %T does not implement json.Marshaler", dynamicValue))
			return
		}
	} else {
		if jitCustomCallKind(callKind) == jitCustomValue && info.directValue {
			// Named maps and single-pointer aggregates occupy the interface
			// data word directly, rather than pointing at their field/header.
			data = *(*unsafe.Pointer)(fieldPtr)
		}
		if tab == nil {
			ctx.setError(errors.New("json: missing custom marshaler itab"))
			return
		}
		face := (*nonEmptyInterface)(unsafe.Pointer(&marshaler))
		face.tab = tab
		face.data = data
	}
	encoded, err := marshaler.MarshalJSON()
	if err != nil {
		ctx.setError(fmt.Errorf("json: error calling MarshalJSON: %w", err))
		return
	}
	if !stdjson.Valid(encoded) {
		ctx.setError(errors.New("json: error calling MarshalJSON: invalid JSON"))
		return
	}
	ctx.ensureCapacity(len(encoded))
	ctx.dst = appendCompactMarshalerJSON(ctx.dst, encoded)
}

func appendCompactMarshalerJSON(dst, src []byte) []byte {
	const hex = "0123456789abcdef"
	inString := false
	escaped := false
	for i := 0; i < len(src); i++ {
		c := src[i]
		if !inString {
			switch c {
			case ' ', '\t', '\r', '\n':
				continue
			case '"':
				inString = true
			}
			dst = append(dst, c)
			continue
		}
		if escaped {
			dst = append(dst, c)
			escaped = false
			continue
		}
		switch c {
		case '\\':
			dst = append(dst, c)
			escaped = true
		case '"':
			dst = append(dst, c)
			inString = false
		case '<', '>', '&':
			dst = append(dst, '\\', 'u', '0', '0', hex[c>>4], hex[c&0x0f])
		case 0xe2:
			if i+2 < len(src) && src[i+1] == 0x80 && (src[i+2] == 0xa8 || src[i+2] == 0xa9) {
				dst = append(dst, '\\', 'u', '2', '0', '2', byte('8'+src[i+2]-0xa8))
				i += 2
			} else {
				dst = append(dst, c)
			}
		default:
			dst = append(dst, c)
		}
	}
	return dst
}

func (p *jitJSONParser) decodeDirectCustom(plan *directDecodePlan, dst unsafe.Pointer) error {
	p.skipSpace()
	start := p.index
	if err := p.skipDirectValue(); err != nil {
		return err
	}
	raw := p.data[start:p.index]
	isNull := len(raw) == 4 && string(raw) == "null"

	var (
		valueType = plan.customType
		data      = dst
		dynamic   bool
	)
	switch plan.custom {
	case jitCustomPointer:
		if isNull {
			*(*unsafe.Pointer)(dst) = nil
			return nil
		}
		ptr := *(*unsafe.Pointer)(dst)
		if ptr == nil {
			ptr = runtimeMallocGC(plan.customType.Elem().Size(), unsafe.Pointer(typePointer(plan.customType.Elem())), true)
			*(*unsafe.Pointer)(dst) = ptr
		}
		data = ptr
	case jitCustomAddress, jitCustomValue:
	case jitCustomInterface:
		dynamic = true
		itab := *(*unsafe.Pointer)(dst)
		if isNull {
			*(*unsafe.Pointer)(dst) = nil
			*(*unsafe.Pointer)(unsafe.Add(dst, 8)) = nil
			return nil
		}
		if itab == nil {
			return p.errorf("cannot unmarshal into nil interface")
		}
		valueType = reflect.TypeOf(interfaceFromHeader(itab, *(*unsafe.Pointer)(unsafe.Add(dst, 8))))
		data = *(*unsafe.Pointer)(unsafe.Add(dst, 8))
		if valueType == nil || data == nil {
			return p.errorf("cannot unmarshal into nil interface")
		}
	default:
		return p.errorf("invalid custom unmarshaler mode")
	}

	var unmarshaler stdjson.Unmarshaler
	var dynamicValue any
	if dynamic {
		face := (*nonEmptyInterface)(unsafe.Pointer(&dynamicValue))
		face.tab = unsafe.Pointer(typePointer(valueType))
		face.data = data
		var ok bool
		unmarshaler, ok = dynamicValue.(stdjson.Unmarshaler)
		if !ok {
			return p.errorf("%s does not implement json.Unmarshaler", valueType)
		}
	} else {
		if plan.customItab == nil {
			return p.errorf("missing custom unmarshaler itab")
		}
		face := (*nonEmptyInterface)(unsafe.Pointer(&unmarshaler))
		face.tab = plan.customItab
		face.data = data
	}
	if err := unmarshaler.UnmarshalJSON(raw); err != nil {
		return p.errorf("error calling UnmarshalJSON for type %s: %v", valueType, err)
	}
	return nil
}

func interfaceFromHeader(itab, data unsafe.Pointer) any {
	var value any
	face := (*nonEmptyInterface)(unsafe.Pointer(&value))
	face.tab = *(*unsafe.Pointer)(unsafe.Add(itab, 8))
	face.data = data
	return value
}
