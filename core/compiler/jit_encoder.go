package compiler

import (
	"encoding/binary"
	"errors"
	"fmt"
	"reflect"
	"runtime"
	"sync"
	"sync/atomic"
	"unsafe"
)

var gEncoder = new(Encoder).init()

var jitEncoderPrograms rcuCache[uintptr, *jitCompiledEncoder]

func loadCompiledEncoder(typ reflect.Type) (*jitCompiledEncoder, error) {
	key := typePointer(typ)
	if program, ok := jitEncoderPrograms.getCompileResult(key); ok {
		return program, nil
	}
	return jitEncoderPrograms.compile(key, func() (*jitCompiledEncoder, error) {
		return compileJITEncoder(typ)
	})
}

var jitEncodeContextPool = sync.Pool{
	New: func() any { return new(jitEncodeContext) },
}

const maxEncodeCapacityHint = 1 << 20

func CompileEncoder(typ reflect.Type, opt uint64) (EncodeHandler, error) {
	return gEncoder.Compile(typ, opt)
}

// EncodeTo encodes a value with the same data-pointer convention as
// CompileEncoder, replacing dst's contents. Passing the slice by value avoids
// forcing a caller's temporary slice header onto the heap.
func EncodeTo(typ reflect.Type, dst []byte, data unsafe.Pointer) ([]byte, error) {
	if typ == nil {
		return nil, errors.New("json: cannot compile a nil type")
	}
	program, err := loadCompiledEncoder(typ)
	if err != nil {
		return nil, err
	}
	return program.encode(dst, data)
}

type Encoder struct {
	rcuCache[reflect.Type, EncodeHandler]
}

func (e *Encoder) init() *Encoder {
	e.rcuCache.init()
	return e
}

func (e *Encoder) Compile(typ reflect.Type, _ uint64) (EncodeHandler, error) {
	if typ == nil {
		return nil, errors.New("json: cannot compile a nil type")
	}
	if handler, ok := e.getCompileResult(typ); ok {
		return handler, nil
	}
	return e.rcuCache.compile(typ, func() (EncodeHandler, error) {
		program, err := loadCompiledEncoder(typ)
		if err != nil {
			return nil, err
		}
		handler := func(buf *[]byte, data unsafe.Pointer) error {
			if buf == nil {
				return errors.New("json: nil destination buffer")
			}
			dst, err := program.encode(*buf, data)
			*buf = dst
			return err
		}
		return handler, nil
	})
}

func (program *jitCompiledEncoder) encode(dst []byte, data unsafe.Pointer) ([]byte, error) {
	dst = dst[:0]
	if data == nil && !program.nilMapMarshaler {
		return append(dst, "null"...), nil
	}
	ctx := jitEncodeContextPool.Get().(*jitEncodeContext)
	hint := program.capacityHint.Load()
	capacity := int(hint)
	if capacity == 0 {
		capacity = program.initialCapacity
	}
	if cap(dst) == 0 {
		// Literal tails may use a final 64-bit store. Reserve its padding
		// as well so an exact size hint does not force a final growth.
		dst = make([]byte, 0, capacity+7)
	}
	*ctx = jitEncodeContext{dst: dst, data: data, program: program.program, mapFrames: ctx.mapFrames}
	if program.rootKind == reflect.Map && !program.mapIndirect {
		ctx.mapValue = data
		ctx.data = unsafe.Pointer(&ctx.mapValue)
	}
	defer func() {
		ctx.releaseMaps()
		*ctx = jitEncodeContext{mapFrames: ctx.mapFrames}
		jitEncodeContextPool.Put(ctx)
	}()
	program.function(ctx)
	if ctx.err == nil {
		nextHint := uint64(min(len(ctx.dst), maxEncodeCapacityHint))
		if nextHint != hint {
			program.capacityHint.Store(nextHint)
		}
	}
	runtime.KeepAlive(ctx)
	runtime.KeepAlive(program)
	return ctx.dst, ctx.err
}

type jitCompiledEncoder struct {
	program         *jitEncodeProgram
	function        func(*jitEncodeContext) uintptr
	initialCapacity int
	rootKind        reflect.Kind
	mapIndirect     bool
	nilMapMarshaler bool
	directInterface bool
	capacityHint    atomic.Uint64 // bounded last output size; unchanged sizes avoid atomic writes
	stats           CodegenStats
}

func compileJITEncoder(typ reflect.Type) (*jitCompiledEncoder, error) {
	compiler := &jitEncoderCompiler{
		unitByType: make(map[jitEncoderUnitKey]int),
	}
	rootType := typ
	pointerDepth := 0
	for rootType.Kind() == reflect.Pointer {
		pointerDepth++
		rootType = rootType.Elem()
	}
	if pointerDepth > 1 {
		return nil, fmt.Errorf("json: JIT encoder supports at most one root pointer level")
	}
	rootAddressable := typ.Kind() == reflect.Pointer
	if _, err := compiler.unitIndex(rootType, rootAddressable); err != nil {
		return nil, err
	}
	for index := 0; index < len(compiler.units); index++ {
		unit := compiler.units[index]
		generator := &jitEncoderGenerator{code: unit.code, compiler: compiler, addressable: unit.addressable}
		if err := generator.emitPrologue(); err != nil {
			return nil, err
		}
		if err := generator.emitValue(unit.typ, 0); err != nil {
			return nil, err
		}
		generator.emitReturn()
	}
	program := newJITEncodeProgram(compiler.static)
	program.mapTypes = compiler.mapTypes
	program.fields = compiler.fields
	program.customTypes = compiler.customTypes
	program.functions = make([]func(*jitEncodeContext) uintptr, len(compiler.units))
	stats := CodegenStats{Functions: len(compiler.units)}
	for index, unit := range compiler.units {
		code, err := unit.code.assemble()
		if err != nil {
			return nil, err
		}
		stats = stats.add(unit.code.stats)
		libFunc := loadJITFunctionFrame(code, fmt.Sprintf("jjs_encoder_%d", index), unit.code.pcsp, int(unit.code.frameSize), unit.code.localPointers())
		program.functions[index] = *(*func(*jitEncodeContext) uintptr)(unsafe.Pointer(&libFunc))
	}
	function := program.functions[0]
	return &jitCompiledEncoder{
		program:         program,
		function:        function,
		initialCapacity: estimateInitialCapacity(rootType, len(compiler.static)),
		rootKind:        rootType.Kind(),
		mapIndirect:     typ.Kind() == reflect.Pointer && rootType.Kind() == reflect.Map,
		nilMapMarshaler: typ.Kind() == reflect.Map && typ.Implements(jitMarshalerType),
		directInterface: typ.Kind() != reflect.Pointer && typ.Kind() != reflect.Map && IsDirectInterfaceType(typ),
		stats:           stats,
	}, nil
}

type jitEncoderCompiler struct {
	static      []byte
	units       []*jitEncoderUnit
	unitByType  map[jitEncoderUnitKey]int
	mapTypes    []reflect.Type
	fields      []jitEncodeFieldMeta
	customTypes []jitCustomTypeInfo
}

type jitEncoderUnitKey struct {
	typ         reflect.Type
	addressable bool
}

type jitEncoderUnit struct {
	typ         reflect.Type
	addressable bool
	code        *jitCodeBuilder
}

func estimateInitialCapacity(typ reflect.Type, staticSize int) int {
	cache := make(map[reflect.Type]int)
	var estimate func(reflect.Type, int) int
	estimate = func(current reflect.Type, depth int) int {
		if current == nil || depth > 8 {
			return 256
		}
		if size, ok := cache[current]; ok {
			return size
		}
		if current.Kind() == reflect.Pointer {
			size := estimate(current.Elem(), depth+1)
			cache[current] = size
			return size
		}
		var size int
		switch current.Kind() {
		case reflect.Bool:
			size = 8
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
			reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
			size = 24
		case reflect.Float32, reflect.Float64:
			size = 32
		case reflect.String:
			size = 64
		case reflect.Struct:
			for i := 0; i < current.NumField(); i++ {
				field := current.Field(i)
				if field.PkgPath != "" {
					continue
				}
				size += len(field.Name) + 6 + estimate(field.Type, depth+1)
			}
		case reflect.Array:
			size = current.Len() * estimate(current.Elem(), depth+1)
		case reflect.Slice, reflect.Map, reflect.Interface:
			size = 192
		default:
			size = 128
		}
		cache[current] = size
		return size
	}
	size := staticSize + estimate(typ, 0)
	if size < 256 {
		size = 256
	}
	if size > 64*1024 {
		size = 64 * 1024
	}
	return size
}

func (c *jitEncoderCompiler) unitIndex(typ reflect.Type, addressable bool) (int, error) {
	key := jitEncoderUnitKey{typ: typ, addressable: addressable}
	if index, ok := c.unitByType[key]; ok {
		return index, nil
	}
	index := len(c.units)
	c.unitByType[key] = index
	jitTypeRegistry.Store(typePointer(typ), typ)
	c.units = append(c.units, &jitEncoderUnit{
		typ:         typ,
		addressable: addressable,
		code:        newJITCodeBuilder(fmt.Sprintf("jjs_encoder_%d", index)),
	})
	return index, nil
}

func (c *jitEncoderCompiler) mapTypeIndex(typ reflect.Type) int {
	c.mapTypes = append(c.mapTypes, typ)
	return len(c.mapTypes) - 1
}

type jitEncoderGenerator struct {
	code        *jitCodeBuilder
	compiler    *jitEncoderCompiler
	addressable bool
	pendingRaw  []byte
	depth       int
	active      map[reflect.Type]int
	returnLabel *jitLabel
}

func (g *jitEncoderGenerator) emitPrologue() error {
	g.code.encoderFrame = true
	g.returnLabel = g.code.newLabel()
	g.code.functionPrologue(jitFrameSize+jitEncoderFrameExtra, int64(unsafe.Offsetof(jitEncodeContext{}.data)))
	return nil
}

func (g *jitEncoderGenerator) emitReturn() {
	g.flushRaw()
	g.code.bind(g.returnLabel)
	g.code.finishFunction(g.code.frameSize)
}

func (g *jitEncoderGenerator) emitValue(typ reflect.Type, offset int64) error {
	if g.active == nil {
		g.active = make(map[reflect.Type]int)
	}
	g.active[typ]++
	defer func() { g.active[typ]-- }()
	if callKind, customType := jitMarshalerCall(typ, g.addressable); callKind != jitCustomNone {
		g.flushRaw()
		typeIndex := g.compiler.customTypeIndex(customType)
		g.code.mov(_R10, _RAX)
		g.code.lea(_R12, offset, _RBX)
		g.code.movConst(int64(callKind), _RCX)
		g.code.movConst(int64(typeIndex), _RDI)
		g.emitHelper(jitHelperCustomMarshal)
		return nil
	}
	if typ.Kind() != reflect.Struct && typ.Kind() != reflect.Array {
		g.flushRaw()
	}
	switch typ.Kind() {
	case reflect.Bool:
		g.emitBool(offset)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		g.emitInteger(offset, typ.Bits()/8, true)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		g.emitInteger(offset, typ.Bits()/8, false)
	case reflect.Float32, reflect.Float64:
		g.emitFloat(offset, typ.Bits()/8)
	case reflect.String:
		g.emitString(offset)
	case reflect.Pointer:
		if g.canInline(typ.Elem()) {
			return g.emitPointer(typ.Elem(), offset)
		}
		childUnit, err := g.compiler.unitIndex(typ.Elem(), true)
		if err != nil {
			return err
		}
		g.code.mov(_R10, _RAX)
		g.code.lea(_R12, offset, _RBX)
		g.code.movConst(int64(childUnit), _RCX)
		g.emitHelper(jitHelperPointer)
	case reflect.Struct:
		return g.emitStruct(typ, offset)
	case reflect.Array:
		if typ.Len() > 16 && g.canInline(typ.Elem()) {
			return g.emitSequence(typ.Elem(), offset, typ.Len())
		}
		if err := g.emitRaw([]byte("[")); err != nil {
			return err
		}
		elemSize := int64(typ.Elem().Size())
		for i := 0; i < typ.Len(); i++ {
			if i > 0 {
				if err := g.emitRaw([]byte(",")); err != nil {
					return err
				}
			}
			if err := g.emitValue(typ.Elem(), offset+int64(i)*elemSize); err != nil {
				return err
			}
		}
		return g.emitRaw([]byte("]"))
	case reflect.Slice:
		if typ.Elem().Kind() == reflect.Uint8 {
			g.emitByteSlice(offset)
			break
		}
		if g.canInline(typ.Elem()) {
			return g.emitSequence(typ.Elem(), offset, -1)
		}
		childUnit, err := g.compiler.unitIndex(typ.Elem(), true)
		if err != nil {
			return err
		}
		g.code.mov(_R10, _RAX)
		g.code.lea(_R12, offset, _RBX)
		g.code.movConst(int64(childUnit), _RCX)
		g.code.movConst(int64(typ.Elem().Size()), _RDI)
		g.emitHelper(jitHelperSlice)
	case reflect.Map:
		return g.emitMap(typ, offset)
	case reflect.Interface:
		g.code.mov(_R10, _RAX)
		g.code.lea(_R12, offset, _RBX)
		if typ.NumMethod() == 0 {
			g.code.xor(_RCX, _RCX)
		} else {
			g.code.movConst(1, _RCX)
		}
		g.emitHelper(jitHelperInterface)
	default:
		return fmt.Errorf("json: JIT encoder does not support %s", typ)
	}
	return nil
}

func (g *jitEncoderGenerator) emitStruct(typ reflect.Type, offset int64) error {
	fields, err := jitExportedFields(typ)
	if err != nil {
		return err
	}
	if len(fields) == 0 {
		return g.emitRaw([]byte("{}"))
	}
	for _, field := range fields {
		if _, static := jitFieldStaticOffset(field.path); field.omitEmpty || !static {
			return g.emitStructDynamic(fields, offset)
		}
	}
	for index, field := range fields {
		staticOffset, _ := jitFieldStaticOffset(field.path)
		fieldOffset := offset + int64(staticOffset)
		prefix := make([]byte, 0, len(field.name)+4)
		if index > 0 {
			prefix = append(prefix, ',')
		} else {
			prefix = append(prefix, '{')
		}
		prefix = appendQuoted(prefix, field.name)
		prefix = append(prefix, ':')
		if err := g.emitRaw(prefix); err != nil {
			return err
		}
		if err := g.emitValue(field.typ, fieldOffset); err != nil {
			return err
		}
	}
	if err := g.emitRaw([]byte("}")); err != nil {
		return err
	}
	return nil
}

func (g *jitEncoderGenerator) emitStructDynamic(fields []jitStructField, offset int64) error {
	if err := g.emitRaw([]byte("{")); err != nil {
		return err
	}
	requiredWritten := false
	for index, field := range fields {
		if staticOffset, direct := jitFieldStaticOffset(field.path); direct && !field.omitEmpty {
			prefix := appendQuoted(nil, field.name)
			prefix = append(prefix, ':')
			switch {
			case requiredWritten:
				if err := g.emitRaw(append([]byte{','}, prefix...)); err != nil {
					return err
				}
			case index == 0:
				if err := g.emitRaw(prefix); err != nil {
					return err
				}
			default:
				// Earlier optional fields may or may not have written a value.
				g.flushRaw()
				metaIndex := len(g.compiler.fields)
				g.compiler.fields = append(g.compiler.fields, jitEncodeFieldMeta{prefix: prefix})
				g.code.mov(_R10, _RAX)
				g.code.movConst(int64(metaIndex), _RBX)
				g.emitHelper(jitHelperFieldPrefix)
			}
			if err := g.emitValue(field.typ, offset+int64(staticOffset)); err != nil {
				return err
			}
			requiredWritten = true
			continue
		}
		customKind, customType := jitMarshalerCall(field.typ, g.addressable)
		childType := field.typ
		pointer := childType.Kind() == reflect.Pointer
		child := 0
		if customKind == jitCustomNone {
			if pointer {
				childType = childType.Elem()
			}
			var err error
			childAddressable := g.addressable
			if pointer {
				childAddressable = true
			}
			child, err = g.compiler.unitIndex(childType, childAddressable)
			if err != nil {
				return err
			}
		}
		prefix := appendQuoted(nil, field.name)
		prefix = append(prefix, ':')
		metaIndex := len(g.compiler.fields)
		g.compiler.fields = append(g.compiler.fields, jitEncodeFieldMeta{
			path:       field.path,
			prefix:     prefix,
			childIndex: child,
			omitEmpty:  field.omitEmpty,
			arrayLen:   fieldArrayLen(field.typ),
			kind:       field.typ.Kind(),
			size:       field.typ.Size(),
			pointer:    pointer,
			customKind: customKind,
			customType: customMetadataIndex(g.compiler, customKind, customType),
		})
		g.flushRaw()
		g.code.mov(_R10, _RAX)
		g.code.lea(_R12, offset, _RBX)
		g.code.movConst(int64(metaIndex), _RCX)
		g.emitHelper(jitHelperResolvedField)
	}
	return g.emitRaw([]byte("}"))
}

func fieldArrayLen(typ reflect.Type) int {
	if typ.Kind() == reflect.Array {
		return typ.Len()
	}
	return 0
}

func customMetadataIndex(compiler *jitEncoderCompiler, kind jitCustomCallKind, typ reflect.Type) int {
	if kind == jitCustomNone {
		return 0
	}
	return compiler.customTypeIndex(typ)
}

func (g *jitEncoderGenerator) emitRaw(value []byte) error {
	g.pendingRaw = append(g.pendingRaw, value...)
	return nil
}

// Adjacent constant fragments (a field prefix and a nested opening brace,
// closing braces, array separators, etc.) share one reserve call and copy.
func (g *jitEncoderGenerator) flushRaw() {
	value := g.pendingRaw
	g.pendingRaw = nil
	if len(value) == 0 {
		return
	}
	offset := len(g.compiler.static)
	g.compiler.static = append(g.compiler.static, value...)
	g.emitReserve(len(value))
	destination := g.code.newVirtualRegister()
	g.code.mov(_RAX, destination)

	used := 0
	if len(value) >= 16 {
		g.code.loadMem(_R10, int64(unsafe.Offsetof(jitEncodeContext{}.program)), _R11)
		g.code.loadMem(_R11, int64(unsafe.Offsetof(jitEncodeProgram{}.static)), _R11)
	}
	for len(value)-used >= 32 {
		g.code.vectorMove32(mem(_R11, int64(offset+used)), mem(destination, int64(used)))
		used += 32
	}
	if len(value)-used >= 16 {
		g.code.vectorMove16(mem(_R11, int64(offset+used)), mem(destination, int64(used)))
		used += 16
	}
	if remaining := len(value) - used; remaining >= 8 {
		g.code.loadImm64(binary.LittleEndian.Uint64(value[used : used+8]))
		g.code.mov(_R11, mem(destination, int64(used)))
		used += 8
	}
	if remaining := len(value) - used; remaining > 0 {
		var tail [8]byte
		copy(tail[:], value[used:])
		g.code.loadImm64(binary.LittleEndian.Uint64(tail[:]))
		g.code.mov(_R11, mem(destination, int64(used)))
	}
}

func (g *jitEncoderGenerator) emitHelper(index int) {
	g.code.loadJITContext(_R11)
	g.code.loadMem(_R11, int64(unsafe.Offsetof(jitEncodeContext{}.program)), _R11)
	g.code.loadMem(_R11, int64(index*8), _R11)
	g.code.call(_R11)
	g.code.restoreJITRegs()
}

func jitJSONField(field reflect.StructField) (name string, omitEmpty, supported bool) {
	tag := field.Tag.Get("json")
	if tag == "-" {
		return "", false, true
	}
	name, options := splitJSONTag(tag)
	if name == "" {
		name = field.Name
	}
	for options != "" {
		var option string
		var found bool
		option, options, found = cutString(options, ",")
		switch option {
		case "":
		case "omitempty":
			omitEmpty = true
		default:
			return "", false, false
		}
		if !found {
			break
		}
	}
	if !isValidJSONTagName(name) {
		name = field.Name
	}
	return name, omitEmpty, true
}
