package compiler

import (
	"encoding/base64"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"unsafe"
)

type directDecodePlan struct {
	typ        reflect.Type
	kind       reflect.Kind
	elem       *directDecodePlan
	fields     []directDecodeField
	byLength   [][]int
	elemType   unsafe.Pointer
	elemSize   uintptr
	typePtr    unsafe.Pointer
	size       uintptr
	nonEmpty   bool
	custom     jitCustomCallKind
	customType reflect.Type
	customItab unsafe.Pointer
}

type directDecodeField struct {
	name     string
	path     []jitFieldStep
	offset   uintptr
	indirect bool
	plan     *directDecodePlan
}

var (
	directDynamicPlans sync.Map
	directDynamicMu    sync.Mutex
)

func compileDirectDecodePlan(typ reflect.Type) (*directDecodePlan, error) {
	return buildDirectDecodePlan(typ, make(map[reflect.Type]*directDecodePlan))
}

func directDecodePlanStats(root *directDecodePlan) (nodes, fields int) {
	seen := make(map[*directDecodePlan]struct{})
	var visit func(*directDecodePlan)
	visit = func(plan *directDecodePlan) {
		if plan == nil {
			return
		}
		if _, ok := seen[plan]; ok {
			return
		}
		seen[plan] = struct{}{}
		nodes++
		fields += len(plan.fields)
		visit(plan.elem)
		for _, field := range plan.fields {
			visit(field.plan)
		}
	}
	visit(root)
	return nodes, fields
}

func buildDirectDecodePlan(typ reflect.Type, cache map[reflect.Type]*directDecodePlan) (*directDecodePlan, error) {
	if plan, ok := cache[typ]; ok {
		return plan, nil
	}
	plan := &directDecodePlan{
		typ:     typ,
		kind:    typ.Kind(),
		typePtr: unsafe.Pointer(typePointer(typ)),
		size:    typ.Size(),
	}
	cache[typ] = plan
	if callKind, customType := jitUnmarshalerCall(typ); callKind != jitCustomNone {
		plan.custom = callKind
		plan.customType = customType
		info := makeJITUnmarshalerTypeInfo(customType)
		plan.customItab = unsafe.Pointer(info.itab)
		return plan, nil
	}

	switch typ.Kind() {
	case reflect.Struct:
		fields, err := jitExportedFields(typ)
		if err != nil {
			return nil, err
		}
		plan.fields = make([]directDecodeField, 0, len(fields))
		maxLength := 0
		for _, field := range fields {
			if len(field.name) > maxLength {
				maxLength = len(field.name)
			}
		}
		plan.byLength = make([][]int, maxLength+1)
		for _, field := range fields {
			child, err := buildDirectDecodePlan(field.typ, cache)
			if err != nil {
				return nil, err
			}
			index := len(plan.fields)
			staticOffset, directPath := jitFieldStaticOffset(field.path)
			plan.fields = append(plan.fields, directDecodeField{
				name:     field.name,
				path:     field.path,
				offset:   staticOffset,
				indirect: !directPath,
				plan:     child,
			})
			plan.byLength[len(field.name)] = append(plan.byLength[len(field.name)], index)
		}
	case reflect.Pointer:
		child, err := buildDirectDecodePlan(typ.Elem(), cache)
		if err != nil {
			return nil, err
		}
		plan.elem = child
	case reflect.Slice:
		child, err := buildDirectDecodePlan(typ.Elem(), cache)
		if err != nil {
			return nil, err
		}
		plan.elem = child
		plan.elemType = unsafe.Pointer(typePointer(typ.Elem()))
		plan.elemSize = typ.Elem().Size()
	case reflect.Array:
		child, err := buildDirectDecodePlan(typ.Elem(), cache)
		if err != nil {
			return nil, err
		}
		plan.elem = child
		plan.elemSize = typ.Elem().Size()
	case reflect.Map:
		if typ.Key().Kind() != reflect.String {
			return nil, fmt.Errorf("json: direct decoder requires string map keys")
		}
		child, err := buildDirectDecodePlan(typ.Elem(), cache)
		if err != nil {
			return nil, err
		}
		plan.elem = child
	case reflect.Interface:
		plan.nonEmpty = typ.NumMethod() != 0
	case reflect.String,
		reflect.Bool,
		reflect.Int,
		reflect.Int8,
		reflect.Int16,
		reflect.Int32,
		reflect.Int64,
		reflect.Uint,
		reflect.Uint8,
		reflect.Uint16,
		reflect.Uint32,
		reflect.Uint64,
		reflect.Uintptr,
		reflect.Float32,
		reflect.Float64:
	default:
		return nil, fmt.Errorf("json: direct decoder does not support %s", typ)
	}
	return plan, nil
}

func (p *jitJSONParser) decodeDirect(plan *directDecodePlan, dst unsafe.Pointer) error {
	if plan.custom != jitCustomNone {
		return p.decodeDirectCustom(plan, dst)
	}
	p.depth++
	if p.depth > 1000 {
		return p.errorf("maximum JSON depth exceeded")
	}
	defer func() { p.depth-- }()
	p.skipSpace()
	if p.consumeLiteral("null") {
		switch plan.kind {
		case reflect.Pointer, reflect.Map:
			*(*unsafe.Pointer)(dst) = nil
		case reflect.Slice:
			*(*sliceHeader)(dst) = sliceHeader{}
		case reflect.Interface:
			*(*unsafe.Pointer)(dst) = nil
			*(*unsafe.Pointer)(unsafe.Add(dst, 8)) = nil
		}
		return nil
	}

	switch plan.kind {
	case reflect.Struct:
		return p.decodeDirectStruct(plan, dst)
	case reflect.String:
		if p.index >= len(p.data) || p.data[p.index] != '"' {
			return p.errorf("expected string")
		}
		value, err := p.parseString()
		if err != nil {
			return err
		}
		*(*string)(dst) = value
		return nil
	case reflect.Bool:
		switch {
		case p.consumeLiteral("true"):
			*(*uint8)(dst) = 1
		case p.consumeLiteral("false"):
			*(*uint8)(dst) = 0
		default:
			return p.errorf("expected bool")
		}
		return nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		text, err := p.parseNumberView()
		if err != nil {
			return err
		}
		value, err := strconv.ParseInt(text, 10, plan.typ.Bits())
		if err != nil {
			return p.errorf("%v", err)
		}
		switch plan.kind {
		case reflect.Int8:
			*(*int8)(dst) = int8(value)
		case reflect.Int16:
			*(*int16)(dst) = int16(value)
		case reflect.Int32:
			*(*int32)(dst) = int32(value)
		case reflect.Int64:
			*(*int64)(dst) = value
		default:
			*(*int)(dst) = int(value)
		}
		return nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		text, err := p.parseNumberView()
		if err != nil {
			return err
		}
		value, err := strconv.ParseUint(text, 10, plan.typ.Bits())
		if err != nil {
			return p.errorf("%v", err)
		}
		switch plan.kind {
		case reflect.Uint8:
			*(*uint8)(dst) = uint8(value)
		case reflect.Uint16:
			*(*uint16)(dst) = uint16(value)
		case reflect.Uint32:
			*(*uint32)(dst) = uint32(value)
		case reflect.Uint64:
			*(*uint64)(dst) = value
		default:
			*(*uint)(dst) = uint(value)
		}
		return nil
	case reflect.Float32, reflect.Float64:
		text, err := p.parseNumberView()
		if err != nil {
			return err
		}
		value, err := strconv.ParseFloat(text, plan.typ.Bits())
		if err != nil {
			return p.errorf("%v", err)
		}
		if plan.kind == reflect.Float32 {
			*(*float32)(dst) = float32(value)
		} else {
			*(*float64)(dst) = value
		}
		return nil
	case reflect.Pointer:
		if *(*unsafe.Pointer)(dst) == nil {
			*(*unsafe.Pointer)(dst) = runtimeMallocGC(plan.elem.size, plan.elem.typePtr, true)
		}
		return p.decodeDirect(plan.elem, *(*unsafe.Pointer)(dst))
	case reflect.Slice:
		if plan.elem.kind == reflect.Uint8 && p.index < len(p.data) && p.data[p.index] == '"' {
			text, err := p.parseString()
			if err != nil {
				return err
			}
			decoded, err := base64.StdEncoding.DecodeString(text)
			if err != nil {
				return err
			}
			*(*sliceHeader)(dst) = *(*sliceHeader)(unsafe.Pointer(&decoded))
			return nil
		}
		return p.decodeDirectSlice(plan, dst)
	case reflect.Array:
		return p.decodeDirectArray(plan, dst)
	case reflect.Map:
		return p.decodeDirectMap(plan, dst)
	case reflect.Interface:
		return p.decodeDirectInterface(plan, dst)
	default:
		return p.errorf("unsupported direct decode type %s", plan.typ)
	}
}

func (p *jitJSONParser) decodeDirectStruct(plan *directDecodePlan, dst unsafe.Pointer) error {
	if p.index >= len(p.data) || p.data[p.index] != '{' {
		return p.errorf("expected object")
	}
	p.index++
	p.skipSpace()
	if p.consumeByte('}') {
		return nil
	}
	for {
		p.skipSpace()
		if p.index >= len(p.data) || p.data[p.index] != '"' {
			return p.errorf("expected object key")
		}
		name, err := p.parseStringKey()
		if err != nil {
			return err
		}
		p.skipSpace()
		if !p.consumeByte(':') {
			return p.errorf("expected ':'")
		}
		if index, ok := plan.lookupField(name); ok {
			field := plan.fields[index]
			fieldPtr := unsafe.Add(dst, field.offset)
			if field.indirect {
				fieldPtr, _ = jitResolveField(dst, field.path, true)
			}
			if err := p.decodeDirect(field.plan, fieldPtr); err != nil {
				return err
			}
		} else if err := p.skipDirectValue(); err != nil {
			return err
		}
		p.skipSpace()
		if p.consumeByte('}') {
			return nil
		}
		if !p.consumeByte(',') {
			return p.errorf("expected ',' or '}'")
		}
	}
}

func (plan *directDecodePlan) lookupField(name string) (int, bool) {
	if len(name) < len(plan.byLength) {
		for _, index := range plan.byLength[len(name)] {
			if plan.fields[index].name == name {
				return index, true
			}
		}
	}
	// Exact matches take priority; folded matches use declaration order. A
	// Unicode fold can change byte length (for example K and the Kelvin sign).
	for index, field := range plan.fields {
		if strings.EqualFold(field.name, name) {
			return index, true
		}
	}
	return 0, false
}

func (p *jitJSONParser) decodeDirectSlice(plan *directDecodePlan, dst unsafe.Pointer) error {
	if p.index >= len(p.data) || p.data[p.index] != '[' {
		return p.errorf("expected array")
	}
	p.index++
	p.skipSpace()
	// Match encoding/json by retaining existing backing storage. Decode into
	// the live elements directly, then clear only elements beyond the new
	// length so stale pointers are not retained.
	header := *(*sliceHeader)(dst)
	previousLen := header.Len
	header.Len = 0
	if p.consumeByte(']') {
		if header.Cap == 0 {
			header.Ptr = runtimeMakeSlice(plan.elemType, 0, 0)
		}
		clearDirectSliceTail(header.Ptr, 0, previousLen, plan.elemSize, plan.elemType)
		*(*sliceHeader)(dst) = header
		return nil
	}
	for {
		if header.Len == header.Cap {
			if header.Cap == 0 {
				header.Ptr = runtimeMakeSlice(plan.elemType, 0, 4)
				header.Cap = 4
			} else {
				header = runtimeGrowSlice(header.Ptr, header.Len+1, header.Cap, 1, plan.elemType)
				header.Len--
			}
		}
		element := unsafe.Add(header.Ptr, uintptr(header.Len)*plan.elemSize)
		if err := p.decodeDirect(plan.elem, element); err != nil {
			return err
		}
		header.Len++
		p.skipSpace()
		if p.consumeByte(']') {
			clearDirectSliceTail(header.Ptr, header.Len, previousLen, plan.elemSize, plan.elemType)
			*(*sliceHeader)(dst) = header
			return nil
		}
		if !p.consumeByte(',') {
			return p.errorf("expected ',' or ']'")
		}
	}
}

func clearDirectSliceTail(ptr unsafe.Pointer, start, end int, elemSize uintptr, elemType unsafe.Pointer) {
	if start >= end {
		return
	}
	for index := start; index < end; index++ {
		runtimeTypedMemclr(elemType, unsafe.Add(ptr, uintptr(index)*elemSize))
	}
}

func (p *jitJSONParser) decodeDirectArray(plan *directDecodePlan, dst unsafe.Pointer) error {
	if p.index >= len(p.data) || p.data[p.index] != '[' {
		return p.errorf("expected array")
	}
	p.index++
	p.skipSpace()
	index := 0
	if p.consumeByte(']') {
		clearDirectSliceTail(dst, 0, plan.typ.Len(), plan.elemSize, plan.elem.typePtr)
		return nil
	}
	for {
		if index < plan.typ.Len() {
			element := unsafe.Add(dst, uintptr(index)*plan.elemSize)
			if err := p.decodeDirect(plan.elem, element); err != nil {
				return err
			}
		} else if err := p.skipDirectValue(); err != nil {
			return err
		}
		index++
		p.skipSpace()
		if p.consumeByte(']') {
			clearDirectSliceTail(dst, index, plan.typ.Len(), plan.elemSize, plan.elem.typePtr)
			return nil
		}
		if !p.consumeByte(',') {
			return p.errorf("expected ',' or ']'")
		}
	}
}

func (p *jitJSONParser) decodeDirectMap(plan *directDecodePlan, dst unsafe.Pointer) error {
	if p.index >= len(p.data) || p.data[p.index] != '{' {
		return p.errorf("expected object")
	}
	p.index++
	// Preserve map capacity and keys not mentioned by this JSON object.
	hmap := *(*unsafe.Pointer)(dst)
	if hmap == nil {
		hmap = runtimeMakeMap(plan.typePtr, 8, nil)
		*(*unsafe.Pointer)(dst) = hmap
	}
	p.skipSpace()
	if p.consumeByte('}') {
		return nil
	}
	for {
		p.skipSpace()
		if p.index >= len(p.data) || p.data[p.index] != '"' {
			return p.errorf("expected object key")
		}
		key, err := p.parseString()
		if err != nil {
			return err
		}
		p.skipSpace()
		if !p.consumeByte(':') {
			return p.errorf("expected ':'")
		}
		header := sliceHeader{
			Ptr: unsafe.Pointer(unsafe.StringData(key)),
			Len: len(key),
			Cap: len(key),
		}
		element := runtimeMapAssign(plan.typePtr, hmap, unsafe.Pointer(&header))
		// Each map value starts from zero, including repeated keys. Reusing the
		// map's buckets must not merge a new value into an old struct or pointer.
		runtimeTypedMemclr(plan.elem.typePtr, element)
		if err := p.decodeDirect(plan.elem, element); err != nil {
			return err
		}
		p.skipSpace()
		if p.consumeByte('}') {
			return nil
		}
		if !p.consumeByte(',') {
			return p.errorf("expected ',' or '}'")
		}
	}
}

func (p *jitJSONParser) decodeDirectInterface(plan *directDecodePlan, dst unsafe.Pointer) error {
	typeWord := *(*unsafe.Pointer)(dst)
	if typeWord != nil {
		typePtr := typeWord
		if plan.nonEmpty {
			typePtr = *(*unsafe.Pointer)(unsafe.Add(typeWord, 8))
		}
		var value any
		dynamic := (*nonEmptyInterface)(unsafe.Pointer(&value))
		dynamic.tab = typePtr
		dynamic.data = *(*unsafe.Pointer)(unsafe.Add(dst, 8))
		typ := reflect.TypeOf(value)
		// Only a non-nil pointer supplies an addressable decode target. Other
		// dynamic values in an empty interface are replaced with generic JSON.
		if typ.Kind() == reflect.Pointer && dynamic.data != nil && !(dynamic.data == dst && typ.Elem() == plan.typ) {
			child, err := loadDirectDecodePlan(typ)
			if err != nil {
				return err
			}
			data := dynamic.data
			return p.decodeDirect(child, unsafe.Pointer(&data))
		}
	}
	if plan.nonEmpty {
		return p.errorf("cannot unmarshal into non-empty interface without a non-nil pointer")
	}
	value, err := p.decodeDirectAny()
	if err != nil {
		return err
	}
	face := (*nonEmptyInterface)(unsafe.Pointer(&value))
	*(*unsafe.Pointer)(dst) = face.tab
	*(*unsafe.Pointer)(unsafe.Add(dst, 8)) = face.data
	return nil
}

func loadDirectDecodePlan(typ reflect.Type) (*directDecodePlan, error) {
	typePtr := typePointer(typ)
	if cached, ok := directDynamicPlans.Load(typePtr); ok {
		return cached.(*directDecodePlan), nil
	}
	directDynamicMu.Lock()
	defer directDynamicMu.Unlock()
	if cached, ok := directDynamicPlans.Load(typePtr); ok {
		return cached.(*directDecodePlan), nil
	}
	plan, err := compileDirectDecodePlan(typ)
	if err != nil {
		return nil, err
	}
	actual, _ := directDynamicPlans.LoadOrStore(typePtr, plan)
	return actual.(*directDecodePlan), nil
}

func (p *jitJSONParser) decodeDirectAny() (any, error) {
	p.depth++
	if p.depth > 1000 {
		return nil, p.errorf("maximum JSON depth exceeded")
	}
	defer func() { p.depth-- }()
	p.skipSpace()
	if p.consumeLiteral("null") {
		return nil, nil
	}
	if p.index >= len(p.data) {
		return nil, p.errorf("unexpected end of JSON input")
	}
	switch p.data[p.index] {
	case '"':
		return p.parseString()
	case 't':
		if !p.consumeLiteral("true") {
			return nil, p.errorf("invalid literal")
		}
		return true, nil
	case 'f':
		if !p.consumeLiteral("false") {
			return nil, p.errorf("invalid literal")
		}
		return false, nil
	case '[':
		p.index++
		values := make([]any, 0, 8)
		p.skipSpace()
		if p.consumeByte(']') {
			return values, nil
		}
		for {
			value, err := p.decodeDirectAny()
			if err != nil {
				return nil, err
			}
			values = append(values, value)
			p.skipSpace()
			if p.consumeByte(']') {
				return values, nil
			}
			if !p.consumeByte(',') {
				return nil, p.errorf("expected ',' or ']'")
			}
		}
	case '{':
		p.index++
		values := make(map[string]any, 8)
		p.skipSpace()
		if p.consumeByte('}') {
			return values, nil
		}
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
			values[key] = value
			p.skipSpace()
			if p.consumeByte('}') {
				return values, nil
			}
			if !p.consumeByte(',') {
				return nil, p.errorf("expected ',' or '}'")
			}
		}
	default:
		text, err := p.parseNumberView()
		if err != nil {
			return nil, err
		}
		value, err := strconv.ParseFloat(text, 64)
		if err != nil {
			return nil, p.errorf("%v", err)
		}
		return value, nil
	}
}

func (p *jitJSONParser) skipDirectValue() error {
	p.depth++
	if p.depth > 1000 {
		return p.errorf("maximum JSON depth exceeded")
	}
	defer func() { p.depth-- }()
	p.skipSpace()
	if p.consumeLiteral("null") || p.consumeLiteral("true") || p.consumeLiteral("false") {
		return nil
	}
	if p.index >= len(p.data) {
		return p.errorf("unexpected end of JSON input")
	}
	switch p.data[p.index] {
	case '"':
		_, err := p.parseStringKey()
		return err
	case '[':
		p.index++
		p.skipSpace()
		if p.consumeByte(']') {
			return nil
		}
		for {
			if err := p.skipDirectValue(); err != nil {
				return err
			}
			p.skipSpace()
			if p.consumeByte(']') {
				return nil
			}
			if !p.consumeByte(',') {
				return p.errorf("expected ',' or ']'")
			}
		}
	case '{':
		p.index++
		p.skipSpace()
		if p.consumeByte('}') {
			return nil
		}
		for {
			p.skipSpace()
			if _, err := p.parseStringKey(); err != nil {
				return err
			}
			p.skipSpace()
			if !p.consumeByte(':') {
				return p.errorf("expected ':'")
			}
			if err := p.skipDirectValue(); err != nil {
				return err
			}
			p.skipSpace()
			if p.consumeByte('}') {
				return nil
			}
			if !p.consumeByte(',') {
				return p.errorf("expected ',' or '}'")
			}
		}
	default:
		return p.consumeNumber()
	}
}

func decodeDirectBytes(data []byte, plan *directDecodePlan, target unsafe.Pointer) error {
	parser := jitJSONParser{data: data}
	if err := parser.decodeDirect(plan, target); err != nil {
		return err
	}
	parser.skipSpace()
	if parser.index != len(data) {
		return parser.errorf("unexpected trailing data")
	}
	return nil
}

//go:linkname runtimeGrowSlice runtime.growslice
func runtimeGrowSlice(oldPtr unsafe.Pointer, newLen, oldCap, num int, elemType unsafe.Pointer) sliceHeader

//go:linkname runtimeTypedMemclr reflect.typedmemclr
func runtimeTypedMemclr(typ, ptr unsafe.Pointer)
