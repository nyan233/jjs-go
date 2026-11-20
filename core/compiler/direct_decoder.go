package compiler

import (
	"fmt"
	"reflect"
	"strconv"
	"sync"
	"unsafe"
)

type directDecodePlan struct {
	typ      reflect.Type
	kind     reflect.Kind
	elem     *directDecodePlan
	fields   []directDecodeField
	byLength [][]int
	elemType unsafe.Pointer
	elemSize uintptr
	typePtr  unsafe.Pointer
	size     uintptr
	nonEmpty bool
}

type directDecodeField struct {
	name   string
	offset uintptr
	plan   *directDecodePlan
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
			plan.fields = append(plan.fields, directDecodeField{
				name:   field.name,
				offset: typ.Field(field.index).Offset,
				plan:   child,
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
		if typ.Elem().Kind() == reflect.Uint8 {
			return nil, fmt.Errorf("json: direct decoder does not support byte slices")
		}
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
			if err := p.decodeDirect(field.plan, unsafe.Add(dst, field.offset)); err != nil {
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
	if len(name) >= len(plan.byLength) {
		return 0, false
	}
	for _, index := range plan.byLength[len(name)] {
		if plan.fields[index].name == name {
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
	header := sliceHeader{}
	if p.consumeByte(']') {
		header.Ptr = runtimeMakeSlice(plan.elemType, 0, 0)
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
			*(*sliceHeader)(dst) = header
			return nil
		}
		if !p.consumeByte(',') {
			return p.errorf("expected ',' or ']'")
		}
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
	hmap := runtimeMakeMap(plan.typePtr, 8, nil)
	*(*unsafe.Pointer)(dst) = hmap
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
		cached, ok := directDynamicPlans.Load(uintptr(typePtr))
		var plan *directDecodePlan
		if ok {
			plan = cached.(*directDecodePlan)
		} else {
			var value any
			dynamic := (*nonEmptyInterface)(unsafe.Pointer(&value))
			dynamic.tab = typeWord
			dynamic.data = *(*unsafe.Pointer)(unsafe.Add(dst, 8))
			typ := reflect.TypeOf(value)
			if typ == nil {
				return p.errorf("invalid interface type")
			}
			var err error
			plan, err = loadDirectDecodePlan(typ)
			if err != nil {
				return err
			}
		}
		data := runtimeMallocGC(plan.size, plan.typePtr, true)
		if err := p.decodeDirect(plan, data); err != nil {
			return err
		}
		*(*unsafe.Pointer)(unsafe.Add(dst, 8)) = data
		return nil
	}
	if plan.nonEmpty {
		return p.errorf("cannot unmarshal into nil non-empty interface")
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
