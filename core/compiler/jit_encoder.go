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

var jitEncodeContextPool = sync.Pool{
	New: func() any { return new(jitEncodeContext) },
}

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

	program, err := compileJITEncoder(typ)
	if err != nil {
		return nil, err
	}
	handler := func(buf *[]byte, data unsafe.Pointer) error {
		if buf == nil {
			return errors.New("json: nil destination buffer")
		}
		if data == nil {
			*buf = append((*buf)[:0], "null"...)
			return nil
		}
		ctx := jitEncodeContextPool.Get().(*jitEncodeContext)
		capacity := program.initialCapacity
		if hint := int(program.capacityHint.Load()); hint > capacity {
			capacity = hint
		}
		dst := (*buf)[:0]
		if cap(dst) < capacity {
			dst = make([]byte, 0, capacity)
		}
		*ctx = jitEncodeContext{
			dst:     dst,
			data:    data,
			program: program.program,
		}
		if program.rootKind == reflect.Map {
			ctx.mapValue = data
			ctx.data = unsafe.Pointer(&ctx.mapValue)
		}
		defer func() {
			*ctx = jitEncodeContext{}
			jitEncodeContextPool.Put(ctx)
		}()
		program.function(ctx)
		*buf = ctx.dst
		if ctx.err == nil {
			program.capacityHint.Store(uint64(len(ctx.dst)))
		}
		runtime.KeepAlive(ctx)
		runtime.KeepAlive(program)
		return ctx.err
	}
	e.setCompileResult(typ, handler)
	return handler, nil
}

type jitCompiledEncoder struct {
	program         *jitEncodeProgram
	function        func(*jitEncodeContext) uintptr
	initialCapacity int
	rootKind        reflect.Kind
	capacityHint    atomic.Uint64 // last successful output size for the next allocation
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
		libFunc := loadJITFunction(code, fmt.Sprintf("jjs_encoder_%d", index), unit.code.pcsp)
		program.functions[index] = *(*func(*jitEncodeContext) uintptr)(unsafe.Pointer(&libFunc))
	}
	function := program.functions[0]
	return &jitCompiledEncoder{
		program:         program,
		function:        function,
		initialCapacity: estimateInitialCapacity(rootType, len(compiler.static)),
		rootKind:        rootType.Kind(),
		stats:           stats,
	}, nil
}

type jitEncoderCompiler struct {
	static      []byte
	units       []*jitEncoderUnit
	unitByType  map[jitEncoderUnitKey]int
	mapTypes    []jitMapType
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
	c.mapTypes = append(c.mapTypes, jitMapType{typePtr: typePointer(typ), key: typ.Key()})
	return len(c.mapTypes) - 1
}

type jitEncoderGenerator struct {
	code        *jitCodeBuilder
	compiler    *jitEncoderCompiler
	addressable bool
}

func (g *jitEncoderGenerator) emitPrologue() error {
	g.code.functionPrologue(jitFrameSize, int64(unsafe.Offsetof(jitEncodeContext{}.data)))
	return nil
}

func (g *jitEncoderGenerator) emitReturn() {
	g.code.finishFunction(jitFrameSize)
}

func (g *jitEncoderGenerator) emitValue(typ reflect.Type, offset int64) error {
	if callKind, customType := jitMarshalerCall(typ, g.addressable); callKind != jitCustomNone {
		typeIndex := g.compiler.customTypeIndex(customType)
		g.code.mov(_R10, _RAX)
		g.code.lea(_R12, offset, _RBX)
		g.code.movConst(int64(callKind), _RCX)
		g.code.movConst(int64(typeIndex), _RDI)
		g.emitHelper(jitHelperCustomMarshal)
		return nil
	}
	switch typ.Kind() {
	case reflect.Bool:
		g.code.lea(_R12, offset, _RBX)
		g.code.mov(_R10, _RAX)
		g.emitHelper(jitHelperBool)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		g.code.lea(_R12, offset, _RBX)
		g.code.mov(_R10, _RAX)
		g.code.movConst(int64(typ.Bits()/8), _RCX)
		g.code.movConst(1, _RDI)
		g.emitHelper(jitHelperInt)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		g.code.lea(_R12, offset, _RBX)
		g.code.mov(_R10, _RAX)
		g.code.movConst(int64(typ.Bits()/8), _RCX)
		g.code.xor(_RDI, _RDI)
		g.emitHelper(jitHelperInt)
	case reflect.Float32, reflect.Float64:
		g.code.lea(_R12, offset, _RBX)
		g.code.mov(_R10, _RAX)
		g.code.movConst(int64(typ.Bits()/8), _RCX)
		g.emitHelper(jitHelperFloat)
	case reflect.String:
		g.code.loadMem(_R12, offset, _RBX)
		g.code.loadMem(_R12, offset+8, _RCX)
		g.code.mov(_R10, _RAX)
		g.emitHelper(jitHelperString)
	case reflect.Pointer:
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
			g.code.mov(_R10, _RAX)
			g.code.lea(_R12, offset, _RBX)
			g.emitHelper(jitHelperByteSlice)
			break
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
		switch typ.Key().Kind() {
		case reflect.String, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
			reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		default:
			return fmt.Errorf("json: JIT encoder does not support map key type %s", typ.Key())
		}
		valueUnit, err := g.compiler.unitIndex(typ.Elem(), false)
		if err != nil {
			return err
		}
		typeIndex := g.compiler.mapTypeIndex(typ)
		g.code.mov(_R10, _RAX)
		g.code.lea(_R12, offset, _RBX)
		g.code.movConst(int64(valueUnit), _RCX)
		g.code.movConst(int64(typeIndex), _RDI)
		g.emitHelper(jitHelperMap)
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
		if _, static := jitFieldStaticOffset(field.path); field.omitEmpty || !static || field.typ.Kind() == reflect.Pointer {
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
	for _, field := range fields {
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
	if len(value) == 0 {
		return nil
	}
	offset := len(g.compiler.static)
	g.compiler.static = append(g.compiler.static, value...)
	g.code.mov(_R10, _RAX)
	g.code.movConst(int64(len(value)), _RBX)
	g.emitHelper(jitHelperReserve)
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
	return nil
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
