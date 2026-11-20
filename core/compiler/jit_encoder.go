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

	golangasm "github.com/twitchyliquid64/golang-asm"
	"github.com/twitchyliquid64/golang-asm/obj"
	"github.com/twitchyliquid64/golang-asm/obj/x86"
)

var gEncoder = new(Encoder).init()

var jitEncodeContextPool = sync.Pool{
	New: func() any { return new(jitEncodeContext) },
}
var jitEncoderScratchRegisters = allocateJITScratchRegisters(3)

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
}

func compileJITEncoder(typ reflect.Type) (*jitCompiledEncoder, error) {
	compiler := &jitEncoderCompiler{
		unitByType: make(map[reflect.Type]int),
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
	if _, err := compiler.unitIndex(rootType); err != nil {
		return nil, err
	}
	for index := 0; index < len(compiler.units); index++ {
		unit := compiler.units[index]
		generator := &jitEncoderGenerator{code: unit.code, compiler: compiler}
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
	program.functions = make([]func(*jitEncodeContext) uintptr, len(compiler.units))
	for index, unit := range compiler.units {
		code, err := unit.code.assemble()
		if err != nil {
			return nil, err
		}
		libFunc := loadJITFunction(code, fmt.Sprintf("jjs_encoder_%d", index))
		program.functions[index] = *(*func(*jitEncodeContext) uintptr)(unsafe.Pointer(&libFunc))
	}
	function := program.functions[0]
	return &jitCompiledEncoder{
		program:         program,
		function:        function,
		initialCapacity: estimateInitialCapacity(rootType, len(compiler.static)),
		rootKind:        rootType.Kind(),
	}, nil
}

type jitEncoderCompiler struct {
	static     []byte
	units      []*jitEncoderUnit
	unitByType map[reflect.Type]int
	mapTypes   []uintptr
}

type jitEncoderUnit struct {
	typ  reflect.Type
	code *jitCodeBuilder
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

func (c *jitEncoderCompiler) unitIndex(typ reflect.Type) (int, error) {
	if index, ok := c.unitByType[typ]; ok {
		return index, nil
	}
	if typ.Kind() == reflect.Pointer {
		return 0, fmt.Errorf("json: JIT encoder unit cannot start at pointer type %s", typ)
	}
	index := len(c.units)
	c.unitByType[typ] = index
	jitTypeRegistry.Store(typePointer(typ), typ)
	c.units = append(c.units, &jitEncoderUnit{
		typ:  typ,
		code: newJITCodeBuilder(fmt.Sprintf("jjs_encoder_%d", index)),
	})
	return index, nil
}

func (c *jitEncoderCompiler) mapTypeIndex(typ reflect.Type) int {
	c.mapTypes = append(c.mapTypes, typePointer(typ))
	return len(c.mapTypes) - 1
}

type jitEncoderGenerator struct {
	code     *jitCodeBuilder
	compiler *jitEncoderCompiler
}

func (g *jitEncoderGenerator) emitPrologue() error {
	g.code.subConst(64, _RSP)
	g.code.mov(_RAX, mem(_RSP, 0))
	g.code.mov(_RAX, _R10)
	g.code.loadMem(_R10, int64(unsafe.Offsetof(jitEncodeContext{}.data)), _R12)
	return nil
}

func (g *jitEncoderGenerator) emitReturn() {
	g.code.addConst(64, _RSP)
	g.code.xor(_RAX, _RAX)
	g.code.ret()
}

func (g *jitEncoderGenerator) emitValue(typ reflect.Type, offset int64) error {
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
		childUnit, err := g.compiler.unitIndex(typ.Elem())
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
		childUnit, err := g.compiler.unitIndex(typ.Elem())
		if err != nil {
			return err
		}
		g.code.mov(_R10, _RAX)
		g.code.lea(_R12, offset, _RBX)
		g.code.movConst(int64(childUnit), _RCX)
		g.code.movConst(int64(typ.Elem().Size()), _RDI)
		g.emitHelper(jitHelperSlice)
	case reflect.Map:
		keyUnit, err := g.compiler.unitIndex(typ.Key())
		if err != nil {
			return err
		}
		valueUnit, err := g.compiler.unitIndex(typ.Elem())
		if err != nil {
			return err
		}
		typeIndex := g.compiler.mapTypeIndex(typ)
		g.code.mov(_R10, _RAX)
		g.code.lea(_R12, offset, _RBX)
		g.code.movConst(int64(keyUnit), _RCX)
		g.code.movConst(int64(valueUnit), _RDI)
		g.code.movConst(int64(typeIndex), _RSI)
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

type jitStructField struct {
	index     int
	name      string
	omitEmpty bool
	typ       reflect.Type
}

func (g *jitEncoderGenerator) emitStruct(typ reflect.Type, offset int64) error {
	fields, err := jitExportedFields(typ)
	if err != nil {
		return err
	}
	if len(fields) == 0 {
		return g.emitRaw([]byte("{}"))
	}
	for index, field := range fields {
		fieldOffset := offset + int64(typ.Field(field.index).Offset)
		if field.omitEmpty {
			return fmt.Errorf("json: JIT encoder does not support omitempty on %s", field.name)
		}
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

func (g *jitEncoderGenerator) emitRaw(value []byte) error {
	if len(value) == 0 {
		return nil
	}
	offset := len(g.compiler.static)
	g.compiler.static = append(g.compiler.static, value...)
	g.code.mov(_R10, _RAX)
	g.code.movConst(int64(len(value)), _RBX)
	g.emitHelper(jitHelperReserve)
	destination := registerAddr(jitEncoderScratchRegisters[0])
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
	if remaining := len(value) - used; remaining >= 4 {
		g.code.movConst(int64(binary.LittleEndian.Uint32(value[used:used+4])), _R11)
		g.code.emit(&obj.Prog{
			As:   x86.AMOVL,
			From: _R11,
			To:   mem(destination, int64(used)),
		})
		used += 4
	}
	if remaining := len(value) - used; remaining >= 2 {
		g.code.movConst(int64(binary.LittleEndian.Uint16(value[used:used+2])), _R11)
		g.code.emit(&obj.Prog{
			As:   x86.AMOVW,
			From: _R11,
			To:   mem(destination, int64(used)),
		})
		used += 2
	}
	if used < len(value) {
		g.code.movConst(int64(value[used]), _R11)
		g.code.emit(&obj.Prog{
			As:   x86.AMOVB,
			From: _R11,
			To:   mem(destination, int64(used)),
		})
	}
	return nil
}

func (g *jitEncoderGenerator) emitHelper(index int) {
	g.code.loadMem(_RSP, 0, _R11)
	g.code.loadMem(_R11, int64(unsafe.Offsetof(jitEncodeContext{}.program)), _R11)
	g.code.loadMem(_R11, int64(index*8), _R11)
	g.code.saveJITRegs()
	g.code.call(_R11)
	g.code.restoreJITRegs()
}

func jitExportedFields(typ reflect.Type) ([]jitStructField, error) {
	fields := make([]jitStructField, 0, typ.NumField())
	names := make(map[string]struct{}, typ.NumField())
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if field.PkgPath != "" {
			continue
		}
		if field.Anonymous {
			return nil, fmt.Errorf("json: JIT encoder does not support embedded field %s", field.Name)
		}
		name, omitEmpty, supported := jitJSONField(field)
		if !supported {
			return nil, fmt.Errorf("json: JIT encoder does not support field option on %s", field.Name)
		}
		if name == "" {
			continue
		}
		if _, exists := names[name]; exists {
			return nil, fmt.Errorf("json: duplicate JSON field name %q", name)
		}
		names[name] = struct{}{}
		fields = append(fields, jitStructField{
			index:     i,
			name:      name,
			omitEmpty: omitEmpty,
			typ:       field.Type,
		})
	}
	return fields, nil
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

type jitCodeBuilder struct {
	name    string
	builder *golangasm.Builder
}

func newJITCodeBuilder(name string) *jitCodeBuilder {
	builder, err := golangasm.NewBuilder("amd64", 64)
	if err != nil {
		panic(err)
	}
	return &jitCodeBuilder{name: name, builder: builder}
}

func (c *jitCodeBuilder) emit(prog *obj.Prog) {
	c.builder.AddInstruction(prog)
}

func (c *jitCodeBuilder) mov(from, to obj.Addr) {
	c.emit(&obj.Prog{As: x86.AMOVQ, From: from, To: to})
}

func (c *jitCodeBuilder) vectorMove32(from, to obj.Addr) {
	c.emit(&obj.Prog{As: x86.AVMOVDQU, From: from, To: _Y0})
	c.emit(&obj.Prog{As: x86.AVMOVDQU, From: _Y0, To: to})
}

func (c *jitCodeBuilder) vectorMove16(from, to obj.Addr) {
	c.emit(&obj.Prog{As: x86.AVMOVDQU, From: from, To: _X0})
	c.emit(&obj.Prog{As: x86.AVMOVDQU, From: _X0, To: to})
}

func (c *jitCodeBuilder) loadImm64(value uint64) {
	c.emit(&obj.Prog{
		As:   x86.AWORD,
		From: obj.Addr{Type: obj.TYPE_CONST, Offset: 0xbb49},
	})
	c.emit(&obj.Prog{
		As:   x86.AQUAD,
		From: obj.Addr{Type: obj.TYPE_CONST, Offset: int64(value)},
	})
}

func (c *jitCodeBuilder) movConst(value int64, to obj.Addr) {
	c.emit(&obj.Prog{As: x86.AMOVQ, From: obj.Addr{Type: obj.TYPE_CONST, Offset: value}, To: to})
}

func (c *jitCodeBuilder) loadMem(base obj.Addr, offset int64, to obj.Addr) {
	c.mov(mem(base, offset), to)
}

func (c *jitCodeBuilder) lea(base obj.Addr, offset int64, to obj.Addr) {
	c.emit(&obj.Prog{As: x86.ALEAQ, From: mem(base, offset), To: to})
}

func (c *jitCodeBuilder) addConst(value int64, to obj.Addr) {
	c.emit(&obj.Prog{As: x86.AADDQ, From: obj.Addr{Type: obj.TYPE_CONST, Offset: value}, To: to})
}

func (c *jitCodeBuilder) subConst(value int64, from obj.Addr) {
	c.emit(&obj.Prog{As: x86.ASUBQ, From: obj.Addr{Type: obj.TYPE_CONST, Offset: value}, To: from})
}

func (c *jitCodeBuilder) xor(from, to obj.Addr) {
	c.emit(&obj.Prog{As: x86.AXORQ, From: from, To: to})
}

func (c *jitCodeBuilder) saveJITRegs() {
	c.mov(_R12, mem(_R10, int64(unsafe.Offsetof(jitEncodeContext{}.saved12))))
	for index, register := range jitEncoderScratchRegisters {
		c.mov(registerAddr(register), mem(_R10, int64(unsafe.Offsetof(jitEncodeContext{}.savedScratch))+int64(index*8)))
	}
}

func (c *jitCodeBuilder) restoreJITRegs() {
	c.loadMem(_RSP, 0, _R10)
	c.loadMem(_R10, int64(unsafe.Offsetof(jitEncodeContext{}.data)), _R12)
	for index, register := range jitEncoderScratchRegisters {
		c.loadMem(_R10, int64(unsafe.Offsetof(jitEncodeContext{}.savedScratch))+int64(index*8), registerAddr(register))
	}
}

func (c *jitCodeBuilder) call(reg obj.Addr) {
	c.emit(&obj.Prog{As: obj.ACALL, To: reg})
}

func (c *jitCodeBuilder) ret() {
	c.emit(&obj.Prog{As: obj.ARET})
}

func (c *jitCodeBuilder) assemble() ([]byte, error) {
	return c.builder.Assemble(), nil
}

func mem(base obj.Addr, offset int64) obj.Addr {
	return obj.Addr{Type: obj.TYPE_MEM, Reg: base.Reg, Offset: offset}
}

func registerAddr(register int) obj.Addr {
	return obj.Addr{Type: obj.TYPE_REG, Reg: int16(register)}
}

var (
	_RAX = obj.Addr{Type: obj.TYPE_REG, Reg: x86.REG_AX}
	_RBX = obj.Addr{Type: obj.TYPE_REG, Reg: x86.REG_BX}
	_RCX = obj.Addr{Type: obj.TYPE_REG, Reg: x86.REG_CX}
	_R8  = obj.Addr{Type: obj.TYPE_REG, Reg: x86.REG_R8}
	_R10 = obj.Addr{Type: obj.TYPE_REG, Reg: x86.REG_R10}
	_R11 = obj.Addr{Type: obj.TYPE_REG, Reg: x86.REG_R11}
	_R12 = obj.Addr{Type: obj.TYPE_REG, Reg: x86.REG_R12}
	_RDI = obj.Addr{Type: obj.TYPE_REG, Reg: x86.REG_DI}
	_RSI = obj.Addr{Type: obj.TYPE_REG, Reg: x86.REG_SI}
	_RSP = obj.Addr{Type: obj.TYPE_REG, Reg: x86.REG_SP}
	_X0  = obj.Addr{Type: obj.TYPE_REG, Reg: x86.REG_X0}
	_Y0  = obj.Addr{Type: obj.TYPE_REG, Reg: x86.REG_Y0}
)
