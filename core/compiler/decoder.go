package compiler

import (
	"encoding/binary"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"sync"
	"unsafe"

	"github.com/twitchyliquid64/golang-asm/obj"
)

var gDecoder = new(Decoder).init()

var jitDecodeContextPool = sync.Pool{
	New: func() any { return new(jitDecodeContext) },
}

func CompileDecoder(typ reflect.Type) (DecodeHandler, error) {
	return gDecoder.Compile(typ)
}

type Decoder struct {
	rcuCache[reflect.Type, DecodeHandler]
}

func (d *Decoder) init() *Decoder {
	d.rcuCache.init()
	return d
}

func (d *Decoder) Compile(typ reflect.Type) (DecodeHandler, error) {
	if typ == nil {
		return nil, errors.New("json: cannot compile a nil type")
	}
	if handler, ok := d.getCompileResult(typ); ok {
		return handler, nil
	}
	return d.rcuCache.compile(typ, func() (DecodeHandler, error) {
		program, err := compileJITDecoder(typ)
		if err != nil {
			return nil, err
		}
		handler := func(data []byte, target unsafe.Pointer) error {
			if target == nil {
				return errors.New("json: nil decode destination")
			}
			if program.direct != nil {
				return decodeDirectBytes(data, program.direct, target)
			}
			root, err := parseJITJSON(data)
			if err != nil {
				return err
			}
			ctx := jitDecodeContextPool.Get().(*jitDecodeContext)
			*ctx = jitDecodeContext{
				root:    root,
				data:    target,
				program: program.program,
			}
			defer func() {
				*ctx = jitDecodeContext{}
				jitDecodeContextPool.Put(ctx)
			}()
			program.function(ctx)
			return ctx.err
		}
		return handler, nil
	})
}

type jitDecodeProgram struct {
	helpers      [8]uintptr
	static       []byte
	functions    []func(*jitDecodeContext) uintptr
	types        []uintptr
	reflectTypes []reflect.Type
	typeSizes    []uintptr
	elemSizes    []uintptr
	elemTypes    []uintptr
}

type jitDecodeContext struct {
	root    *jitJSONNode
	data    unsafe.Pointer
	program *jitDecodeProgram
	err     error
}

type jitCompiledDecoder struct {
	program  *jitDecodeProgram
	function func(*jitDecodeContext) uintptr
	stats    CodegenStats
	direct   *directDecodePlan
	size     uintptr
	typePtr  uintptr
}

func newJITDecodeProgram(static []byte) *jitDecodeProgram {
	return &jitDecodeProgram{
		helpers: [8]uintptr{
			reflect.ValueOf(jitDecodeString).Pointer(),
			reflect.ValueOf(jitDecodeInteger).Pointer(),
			reflect.ValueOf(jitDecodeFloat).Pointer(),
			reflect.ValueOf(jitDecodeBool).Pointer(),
			reflect.ValueOf(jitDecodePointerField).Pointer(),
			reflect.ValueOf(jitDecodeSliceField).Pointer(),
			reflect.ValueOf(jitDecodeMapField).Pointer(),
			reflect.ValueOf(jitDecodeInterfaceField).Pointer(),
		},
		static: static,
	}
}

func compileJITDecoder(typ reflect.Type) (*jitCompiledDecoder, error) {
	direct, directErr := compileDirectDecodePlan(typ)
	if directErr == nil {
		stats := CodegenStats{DirectDecoder: true}
		stats.DirectPlanNodes, stats.DirectFields = directDecodePlanStats(direct)
		return &jitCompiledDecoder{direct: direct, stats: stats}, nil
	}
	compiler := &jitDecoderCompiler{unitByType: make(map[reflect.Type]int)}
	if _, err := compiler.unitIndex(typ); err != nil {
		return nil, err
	}
	for index := 0; index < len(compiler.units); index++ {
		unit := compiler.units[index]
		generator := &jitDecoderGenerator{code: unit.code, compiler: compiler}
		if err := generator.emitPrologue(); err != nil {
			return nil, err
		}
		if err := generator.emitValue(unit.typ, 0, nil); err != nil {
			return nil, err
		}
		generator.emitReturn()
	}
	program := newJITDecodeProgram(compiler.static)
	program.types = compiler.types
	program.reflectTypes = compiler.reflectTypes
	program.typeSizes = compiler.typeSizes
	program.elemSizes = compiler.elemSizes
	program.elemTypes = compiler.elemTypes
	program.functions = make([]func(*jitDecodeContext) uintptr, len(compiler.units))
	stats := CodegenStats{Functions: len(compiler.units)}
	for index, unit := range compiler.units {
		code, err := unit.code.assemble()
		if err != nil {
			return nil, err
		}
		stats = stats.add(unit.code.stats)
		libFunc := loadJITFunction(code, fmt.Sprintf("jjs_decoder_%d", index), unit.code.pcsp)
		program.functions[index] = *(*func(*jitDecodeContext) uintptr)(unsafe.Pointer(&libFunc))
	}
	function := program.functions[0]
	return &jitCompiledDecoder{
		program:  program,
		function: function,
		stats:    stats,
		size:     typ.Size(),
		typePtr:  typePointer(typ),
	}, nil
}

type jitDecoderCompiler struct {
	static       []byte
	units        []*jitDecoderUnit
	unitByType   map[reflect.Type]int
	types        []uintptr
	reflectTypes []reflect.Type
	typeSizes    []uintptr
	elemSizes    []uintptr
	elemTypes    []uintptr
}

type jitDecoderUnit struct {
	typ  reflect.Type
	code *jitCodeBuilder
}

func (c *jitDecoderCompiler) unitIndex(typ reflect.Type) (int, error) {
	if index, ok := c.unitByType[typ]; ok {
		return index, nil
	}
	index := len(c.units)
	c.unitByType[typ] = index
	jitTypeRegistry.Store(typePointer(typ), typ)
	c.units = append(c.units, &jitDecoderUnit{
		typ:  typ,
		code: newJITCodeBuilder(fmt.Sprintf("jjs_decoder_%d", index)),
	})
	return index, nil
}

func (c *jitDecoderCompiler) typeIndex(typ reflect.Type) int {
	c.types = append(c.types, typePointer(typ))
	c.reflectTypes = append(c.reflectTypes, typ)
	c.typeSizes = append(c.typeSizes, typ.Size())
	elemSize := uintptr(0)
	elemType := uintptr(0)
	if typ.Kind() == reflect.Slice {
		elemSize = typ.Elem().Size()
		elemType = typePointer(typ.Elem())
	}
	c.elemSizes = append(c.elemSizes, elemSize)
	c.elemTypes = append(c.elemTypes, elemType)
	return len(c.types) - 1
}

func typePointer(typ reflect.Type) uintptr {
	return uintptr(runtimeTypePointer(typ))
}

// Preserve pointer provenance for heap-backed types created by reflect.
func runtimeTypePointer(typ reflect.Type) unsafe.Pointer {
	face := (*nonEmptyInterface)(unsafe.Pointer(&typ))
	return face.data
}

type nonEmptyInterface struct {
	tab  unsafe.Pointer
	data unsafe.Pointer
}

type jitDecoderGenerator struct {
	code     *jitCodeBuilder
	compiler *jitDecoderCompiler
}

func (g *jitDecoderGenerator) emitPrologue() error {
	g.code.functionPrologue(jitFrameSize, int64(unsafe.Offsetof(jitDecodeContext{}.data)))
	return nil
}

func (g *jitDecoderGenerator) emitReturn() {
	g.code.finishFunction(jitFrameSize)
}

type jitPathSegment struct {
	field string
	index int
	array bool
}

func (g *jitDecoderGenerator) emitValue(typ reflect.Type, offset int64, path []jitPathSegment) error {
	switch typ.Kind() {
	case reflect.Struct:
		return g.emitStruct(typ, offset, path)
	case reflect.String:
		return g.emitDecodeCall(0, typ, offset, path)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return g.emitDecodeInteger(typ, offset, path)
	case reflect.Float32, reflect.Float64:
		return g.emitDecodeCall(2, typ, offset, path)
	case reflect.Bool:
		return g.emitDecodeCall(3, typ, offset, path)
	case reflect.Pointer:
		return g.emitPointer(typ, offset, path)
	case reflect.Slice:
		return g.emitSlice(typ, offset, path)
	case reflect.Map:
		return g.emitMap(typ, offset, path)
	case reflect.Interface:
		return g.emitDecodeCall(7, typ, offset, path)
	case reflect.Array:
		size := int64(typ.Elem().Size())
		for i := 0; i < typ.Len(); i++ {
			child := append(append([]jitPathSegment(nil), path...), jitPathSegment{array: true, index: i})
			if err := g.emitValue(typ.Elem(), offset+int64(i)*size, child); err != nil {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf("json: JIT decoder does not support %s", typ)
	}
}

func (g *jitDecoderGenerator) emitPointer(typ reflect.Type, offset int64, path []jitPathSegment) error {
	child, err := g.compiler.unitIndex(typ.Elem())
	if err != nil {
		return err
	}
	typeIndex := g.compiler.typeIndex(typ.Elem())
	pathOffset, pathLen := g.addPath(path)
	g.code.reloadDecodeContext()
	g.code.lea(_R12, offset, _RBX)
	g.code.mov(_R10, _RAX)
	g.code.loadProgramStatic(_R10, int64(unsafe.Offsetof(jitDecodeContext{}.program)), pathOffset, _RCX)
	g.code.movConst(int64(pathLen), _RDI)
	g.code.movConst(int64(child), _RSI)
	g.code.movConst(int64(typeIndex), _R8)
	g.emitHelper(4)
	return nil
}

func (g *jitDecoderGenerator) emitSlice(typ reflect.Type, offset int64, path []jitPathSegment) error {
	child, err := g.compiler.unitIndex(typ.Elem())
	if err != nil {
		return err
	}
	typeIndex := g.compiler.typeIndex(typ)
	pathOffset, pathLen := g.addPath(path)
	g.code.reloadDecodeContext()
	g.code.lea(_R12, offset, _RBX)
	g.code.mov(_R10, _RAX)
	g.code.loadProgramStatic(_R10, int64(unsafe.Offsetof(jitDecodeContext{}.program)), pathOffset, _RCX)
	g.code.movConst(int64(pathLen), _RDI)
	g.code.movConst(int64(child), _RSI)
	g.code.movConst(int64(typeIndex), _R8)
	g.emitHelper(5)
	return nil
}

func (g *jitDecoderGenerator) emitMap(typ reflect.Type, offset int64, path []jitPathSegment) error {
	if !supportsJITMapKeyDecode(typ.Key()) {
		return fmt.Errorf("json: JIT decoder does not support map key type %s", typ.Key())
	}
	child, err := g.compiler.unitIndex(typ.Elem())
	if err != nil {
		return err
	}
	typeIndex := g.compiler.typeIndex(typ)
	pathOffset, pathLen := g.addPath(path)
	g.code.reloadDecodeContext()
	g.code.lea(_R12, offset, _RBX)
	g.code.mov(_R10, _RAX)
	g.code.loadProgramStatic(_R10, int64(unsafe.Offsetof(jitDecodeContext{}.program)), pathOffset, _RCX)
	g.code.movConst(int64(pathLen), _RDI)
	g.code.movConst(int64(child), _RSI)
	g.code.movConst(int64(typeIndex), _R8)
	g.emitHelper(6)
	return nil
}

func (g *jitDecoderGenerator) emitStruct(typ reflect.Type, offset int64, path []jitPathSegment) error {
	fields, err := jitExportedFields(typ)
	if err != nil {
		return err
	}
	for _, field := range fields {
		staticOffset, ok := jitFieldStaticOffset(field.path)
		if !ok {
			return fmt.Errorf("json: legacy JIT decoder cannot address embedded pointer field %s", field.name)
		}
		childPath := append(append([]jitPathSegment(nil), path...), jitPathSegment{field: field.name})
		if err := g.emitValue(field.typ, offset+int64(staticOffset), childPath); err != nil {
			return err
		}
	}
	return nil
}

func (g *jitDecoderGenerator) emitDecodeInteger(typ reflect.Type, offset int64, path []jitPathSegment) error {
	pathOffset, pathLen := g.addPath(path)
	g.code.reloadDecodeContext()
	g.code.lea(_R12, offset, _RBX)
	g.code.mov(_R10, _RAX)
	g.code.loadProgramStatic(_R10, int64(unsafe.Offsetof(jitDecodeContext{}.program)), pathOffset, _RCX)
	g.code.movConst(int64(pathLen), _RDI)
	g.code.movConst(int64(typ.Bits()/8), _RSI)
	signed := int64(0)
	if typ.Kind() >= reflect.Int && typ.Kind() <= reflect.Int64 {
		signed = 1
	}
	g.code.movConst(signed, _R8)
	g.emitHelper(1)
	return nil
}

func (g *jitDecoderGenerator) emitDecodeCall(helper int, typ reflect.Type, offset int64, path []jitPathSegment) error {
	pathOffset, pathLen := g.addPath(path)
	g.code.reloadDecodeContext()
	g.code.lea(_R12, offset, _RBX)
	g.code.mov(_R10, _RAX)
	g.code.loadProgramStatic(_R10, int64(unsafe.Offsetof(jitDecodeContext{}.program)), pathOffset, _RCX)
	g.code.movConst(int64(pathLen), _RDI)
	if helper == 2 {
		g.code.movConst(int64(typ.Bits()/8), _RSI)
	}
	if helper == 7 && typ.NumMethod() > 0 {
		g.code.movConst(1, _RSI)
	} else if helper == 7 {
		g.code.xor(_RSI, _RSI)
	}
	g.emitHelper(helper)
	return nil
}

func (g *jitDecoderGenerator) addPath(path []jitPathSegment) (int, int) {
	offset := len(g.compiler.static)
	for _, segment := range path {
		if segment.array {
			g.compiler.static = append(g.compiler.static, 1, 4, 0)
			payload := len(g.compiler.static)
			g.compiler.static = append(g.compiler.static, 0, 0, 0, 0)
			binary.LittleEndian.PutUint32(g.compiler.static[payload:payload+4], uint32(segment.index))
			continue
		}
		if len(segment.field) > 0xffff {
			panic("JSON field name is too long")
		}
		g.compiler.static = append(g.compiler.static, 0, byte(len(segment.field)), byte(len(segment.field)>>8))
		g.compiler.static = append(g.compiler.static, segment.field...)
	}
	return offset, len(g.compiler.static) - offset
}

func (g *jitDecoderGenerator) emitHelper(index int) {
	g.code.loadJITContext(_R11)
	g.code.loadMem(_R11, int64(unsafe.Offsetof(jitDecodeContext{}.program)), _R11)
	g.code.loadMem(_R11, int64(index*8), _R11)
	g.code.call(_R11)
}

func (c *jitCodeBuilder) loadProgramStatic(context obj.Addr, programOffset int64, staticOffset int, to obj.Addr) {
	c.loadMem(context, programOffset, _R11)
	c.loadMem(_R11, int64(unsafe.Offsetof(jitDecodeProgram{}.static)), to)
	c.addConst(int64(staticOffset), to)
}

func (c *jitCodeBuilder) reloadDecodeContext() {
	c.loadJITContext(_R10)
	c.loadMem(_R10, int64(unsafe.Offsetof(jitDecodeContext{}.data)), _R12)
}

func jitDecodeString(ctx *jitDecodeContext, dst unsafe.Pointer, path unsafe.Pointer, pathLen int) {
	node := resolveJITPath(ctx.root, path, pathLen)
	if node == nil || node.kind == jitJSONNull {
		return
	}
	if node.kind != jitJSONString {
		setJITDecodeError(ctx, "expected string")
		return
	}
	*(*string)(dst) = node.text
}

func jitDecodeInteger(ctx *jitDecodeContext, dst unsafe.Pointer, path unsafe.Pointer, pathLen, width, signed int) {
	node := resolveJITPath(ctx.root, path, pathLen)
	if node == nil || node.kind == jitJSONNull {
		return
	}
	if node.kind != jitJSONNumber {
		setJITDecodeError(ctx, "expected number")
		return
	}
	if signed != 0 {
		value, err := strconv.ParseInt(node.text, 10, width*8)
		if err != nil {
			setJITDecodeError(ctx, err.Error())
			return
		}
		switch width {
		case 1:
			*(*int8)(dst) = int8(value)
		case 2:
			*(*int16)(dst) = int16(value)
		case 4:
			*(*int32)(dst) = int32(value)
		case 8:
			*(*int64)(dst) = value
		}
		return
	}
	value, err := strconv.ParseUint(node.text, 10, width*8)
	if err != nil {
		setJITDecodeError(ctx, err.Error())
		return
	}
	switch width {
	case 1:
		*(*uint8)(dst) = uint8(value)
	case 2:
		*(*uint16)(dst) = uint16(value)
	case 4:
		*(*uint32)(dst) = uint32(value)
	case 8:
		*(*uint64)(dst) = value
	}
}

func jitDecodeFloat(ctx *jitDecodeContext, dst unsafe.Pointer, path unsafe.Pointer, pathLen, width int) {
	node := resolveJITPath(ctx.root, path, pathLen)
	if node == nil || node.kind == jitJSONNull {
		return
	}
	if node.kind != jitJSONNumber {
		setJITDecodeError(ctx, "expected number")
		return
	}
	value, err := strconv.ParseFloat(node.text, width*8)
	if err != nil {
		setJITDecodeError(ctx, err.Error())
		return
	}
	if width == 4 {
		*(*float32)(dst) = float32(value)
	} else {
		*(*float64)(dst) = value
	}
}

func jitDecodeBool(ctx *jitDecodeContext, dst unsafe.Pointer, path unsafe.Pointer, pathLen int) {
	node := resolveJITPath(ctx.root, path, pathLen)
	if node == nil || node.kind == jitJSONNull {
		return
	}
	if node.kind != jitJSONBool {
		setJITDecodeError(ctx, "expected bool")
		return
	}
	if node.boolean {
		*(*uint8)(dst) = 1
	} else {
		*(*uint8)(dst) = 0
	}
}

func resolveJITPath(root *jitJSONNode, path unsafe.Pointer, pathLen int) *jitJSONNode {
	node := root
	for offset := 0; offset < pathLen; {
		if offset+3 > pathLen {
			return nil
		}
		kind := *(*byte)(unsafe.Add(path, offset))
		size := int(*(*uint16)(unsafe.Add(path, offset+1)))
		offset += 3
		if offset+size > pathLen {
			return nil
		}
		switch kind {
		case 0:
			name := unsafe.String((*byte)(unsafe.Add(path, offset)), size)
			if node.kind != jitJSONObject {
				return nil
			}
			found := false
			for i := len(node.object) - 1; i >= 0; i-- {
				if node.object[i].name == name {
					node = node.object[i].value
					found = true
					break
				}
			}
			if !found {
				return nil
			}
		case 1:
			if size != 4 || node.kind != jitJSONArray {
				return nil
			}
			index := int(*(*uint32)(unsafe.Add(path, offset)))
			if index >= len(node.array) {
				return nil
			}
			node = node.array[index]
		default:
			return nil
		}
		offset += size
	}
	return node
}

func setJITDecodeError(ctx *jitDecodeContext, message string) {
	if ctx.err == nil {
		ctx.err = errors.New("json: " + message)
	}
}

//go:noinline
func jitDecodePointerField(ctx *jitDecodeContext, dst unsafe.Pointer, path unsafe.Pointer, pathLen, childIndex, typeIndex int) {
	node := resolveJITPath(ctx.root, path, pathLen)
	if node == nil {
		return
	}
	if node.kind == jitJSONNull {
		*(*unsafe.Pointer)(dst) = nil
		return
	}
	if typeIndex < 0 || typeIndex >= len(ctx.program.types) {
		setJITDecodeError(ctx, "invalid pointer type index")
		return
	}
	ptr := runtimeMallocGC(ctx.program.typeSizes[typeIndex], unsafe.Pointer(ctx.program.types[typeIndex]), true)
	previousRoot := ctx.root
	previousData := ctx.data
	ctx.root = node
	ctx.data = ptr
	ctx.program.functions[childIndex](ctx)
	ctx.root = previousRoot
	ctx.data = previousData
	if ctx.err == nil {
		*(*unsafe.Pointer)(dst) = ptr
	}
}

//go:noinline
func jitDecodeSliceField(ctx *jitDecodeContext, dst unsafe.Pointer, path unsafe.Pointer, pathLen, childIndex, typeIndex int) {
	node := resolveJITPath(ctx.root, path, pathLen)
	if node == nil {
		return
	}
	if node.kind == jitJSONNull {
		*(*sliceHeader)(dst) = sliceHeader{}
		return
	}
	if node.kind != jitJSONArray {
		setJITDecodeError(ctx, "expected array")
		return
	}
	ptr := runtimeMakeSlice(unsafe.Pointer(ctx.program.elemTypes[typeIndex]), len(node.array), len(node.array))
	header := sliceHeader{Ptr: ptr, Len: len(node.array), Cap: len(node.array)}
	*(*sliceHeader)(dst) = header
	elemSize := int(ctx.program.elemSizes[typeIndex])
	for i, child := range node.array {
		previousRoot := ctx.root
		previousData := ctx.data
		ctx.root = child
		ctx.data = unsafe.Add(ptr, uintptr(i*elemSize))
		ctx.program.functions[childIndex](ctx)
		ctx.root = previousRoot
		ctx.data = previousData
		if ctx.err != nil {
			return
		}
	}
}

//go:noinline
func jitDecodeMapField(ctx *jitDecodeContext, dst unsafe.Pointer, path unsafe.Pointer, pathLen, childIndex, typeIndex int) {
	node := resolveJITPath(ctx.root, path, pathLen)
	if node == nil {
		return
	}
	if node.kind == jitJSONNull {
		*(*unsafe.Pointer)(dst) = nil
		return
	}
	if node.kind != jitJSONObject {
		setJITDecodeError(ctx, "expected object")
		return
	}
	mapType := unsafe.Pointer(ctx.program.types[typeIndex])
	hmap := runtimeMakeMap(mapType, len(node.object), nil)
	*(*unsafe.Pointer)(dst) = hmap
	keyType := ctx.program.reflectTypes[typeIndex].Key()
	keyText := reflect.PointerTo(keyType).Implements(jitTextUnmarshalerType)
	for _, field := range node.object {
		elem, err := assignDecodedMapKey(mapType, hmap, keyType, keyText, field.name)
		if err != nil {
			if ctx.err == nil {
				ctx.err = err
			}
			return
		}
		previousRoot := ctx.root
		previousData := ctx.data
		ctx.root = field.value
		ctx.data = elem
		ctx.program.functions[childIndex](ctx)
		ctx.root = previousRoot
		ctx.data = previousData
		if ctx.err != nil {
			return
		}
	}
}

type sliceHeader struct {
	Ptr unsafe.Pointer
	Len int
	Cap int
}

var jitDynamicDecoderCache sync.Map
var jitDynamicDecodeCompileMu sync.Mutex

//go:noinline
func jitDecodeInterfaceField(ctx *jitDecodeContext, dst unsafe.Pointer, path unsafe.Pointer, pathLen, nonEmpty int) {
	node := resolveJITPath(ctx.root, path, pathLen)
	if node == nil {
		return
	}
	typeWord := *(*unsafe.Pointer)(dst)
	if typeWord != nil {
		typePtr := typeWord
		if nonEmpty != 0 {
			typePtr = *(*unsafe.Pointer)(unsafe.Add(typeWord, 8))
		}
		decoded, ok := jitDynamicDecoderCache.Load(uintptr(typePtr))
		var child *jitCompiledDecoder
		var typ reflect.Type
		if ok {
			child = decoded.(*jitCompiledDecoder)
		} else {
			var value any
			dynamic := (*nonEmptyInterface)(unsafe.Pointer(&value))
			dynamic.tab = typePtr
			dynamic.data = *(*unsafe.Pointer)(unsafe.Add(dst, 8))
			typ = reflect.TypeOf(value)
			if typ == nil {
				setJITDecodeError(ctx, "invalid interface type")
				return
			}
			jitDynamicDecodeCompileMu.Lock()
			if decoded, ok := jitDynamicDecoderCache.Load(uintptr(typePtr)); ok {
				child = decoded.(*jitCompiledDecoder)
				jitDynamicDecodeCompileMu.Unlock()
			} else {
				compiled, err := compileJITDecoder(typ)
				if err != nil {
					jitDynamicDecodeCompileMu.Unlock()
					setJITDecodeError(ctx, err.Error())
					return
				}
				actual, _ := jitDynamicDecoderCache.LoadOrStore(uintptr(typePtr), compiled)
				child = actual.(*jitCompiledDecoder)
				jitDynamicDecodeCompileMu.Unlock()
			}
		}
		valueData := runtimeMallocGC(child.size, unsafe.Pointer(child.typePtr), true)
		*(*unsafe.Pointer)(unsafe.Add(dst, 8)) = valueData
		childContext := jitDecodeContextPool.Get().(*jitDecodeContext)
		*childContext = jitDecodeContext{
			root:    node,
			data:    valueData,
			program: child.program,
		}
		defer func() {
			*childContext = jitDecodeContext{}
			jitDecodeContextPool.Put(childContext)
		}()
		child.function(childContext)
		if childContext.err != nil {
			ctx.err = childContext.err
		}
		return
	}
	var value any = jitNodeToAny(node)
	face := (*nonEmptyInterface)(unsafe.Pointer(&value))
	*(*unsafe.Pointer)(dst) = face.tab
	*(*unsafe.Pointer)(unsafe.Add(dst, 8)) = face.data
}

func jitNodeToAny(node *jitJSONNode) any {
	if node == nil {
		return nil
	}
	switch node.kind {
	case jitJSONNull:
		return nil
	case jitJSONBool:
		return node.boolean
	case jitJSONNumber:
		value, err := strconv.ParseFloat(node.text, 64)
		if err != nil {
			return nil
		}
		return value
	case jitJSONString:
		return node.text
	case jitJSONArray:
		value := make([]any, len(node.array))
		for i, child := range node.array {
			value[i] = jitNodeToAny(child)
		}
		return value
	case jitJSONObject:
		value := make(map[string]any, len(node.object))
		for _, field := range node.object {
			value[field.name] = jitNodeToAny(field.value)
		}
		return value
	default:
		return nil
	}
}

//go:linkname runtimeMallocGC runtime.mallocgc
func runtimeMallocGC(size uintptr, typ unsafe.Pointer, needZero bool) unsafe.Pointer

//go:linkname runtimeMakeSlice runtime.makeslice
func runtimeMakeSlice(elemType unsafe.Pointer, length, capacity int) unsafe.Pointer

//go:linkname runtimeMakeMap runtime.makemap
func runtimeMakeMap(mapType unsafe.Pointer, hint int, hmap unsafe.Pointer) unsafe.Pointer

//go:linkname runtimeMapAssign runtime.mapassign
func runtimeMapAssign(mapType, hmap, key unsafe.Pointer) unsafe.Pointer
