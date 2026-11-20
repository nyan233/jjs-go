package compiler

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"reflect"
	"runtime"
	"sync"
	"unsafe"

	golangasm "github.com/twitchyliquid64/golang-asm"
	"github.com/twitchyliquid64/golang-asm/obj"
	"github.com/twitchyliquid64/golang-asm/obj/x86"
)

var gEncoder = new(Encoder).init()

var jitEncodeContexts sync.Map

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
		ctx := &jitEncodeContext{
			dst:     (*buf)[:0],
			data:    data,
			program: program.program,
		}
		jitEncodeContexts.Store(ctx, struct{}{})
		defer jitEncodeContexts.Delete(ctx)
		program.function(ctx)
		*buf = ctx.dst
		runtime.KeepAlive(ctx)
		runtime.KeepAlive(program)
		return ctx.err
	}
	e.setCompileResult(typ, handler)
	return handler, nil
}

type jitCompiledEncoder struct {
	program  *jitEncodeProgram
	function func(*jitEncodeContext) uintptr
}

func compileJITEncoder(typ reflect.Type) (*jitCompiledEncoder, error) {
	generator := &jitEncoderGenerator{
		code: newJITCodeBuilder("jjs_encoder"),
	}
	rootType := typ
	pointerDepth := 0
	for rootType.Kind() == reflect.Pointer {
		pointerDepth++
		rootType = rootType.Elem()
	}
	if err := generator.emitPrologue(); err != nil {
		return nil, err
	}
	if pointerDepth > 0 {
		for i := 1; i < pointerDepth; i++ {
			generator.code.loadMem(_R12, 0, _R12)
		}
		if err := generator.emitValue(rootType, 0); err != nil {
			return nil, err
		}
	} else if err := generator.emitValue(rootType, 0); err != nil {
		return nil, err
	}
	generator.emitReturn()

	code, err := generator.code.assemble()
	if err != nil {
		return nil, err
	}
	if Debug {
		logger.Debug(hex.EncodeToString(code))
	}
	program := newJITEncodeProgram(generator.static)
	program.helpers = generator.helpers()
	libFunc := jitLoader.LoadOne(code, "jjs_encoder", 64, 16, []bool{true, false}, []bool{true, false, false, false, false, false, false, false})
	function := *(*func(*jitEncodeContext) uintptr)(unsafe.Pointer(&libFunc))
	return &jitCompiledEncoder{
		program:  program,
		function: function,
	}, nil
}

type jitEncoderGenerator struct {
	code   *jitCodeBuilder
	static []byte
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

func (g *jitEncoderGenerator) helpers() [8]uintptr {
	return newJITEncodeProgram(nil).helpers
}

func (g *jitEncoderGenerator) emitValue(typ reflect.Type, offset int64) error {
	switch typ.Kind() {
	case reflect.Bool:
		g.code.lea(_R12, offset, _RBX)
		g.code.mov(_R10, _RAX)
		g.emitHelper(4)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		g.code.lea(_R12, offset, _RBX)
		g.code.mov(_R10, _RAX)
		g.code.movConst(int64(typ.Bits()/8), _RCX)
		g.code.movConst(1, _RDI)
		g.emitHelper(2)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		g.code.lea(_R12, offset, _RBX)
		g.code.mov(_R10, _RAX)
		g.code.movConst(int64(typ.Bits()/8), _RCX)
		g.code.xor(_RDI, _RDI)
		g.emitHelper(2)
	case reflect.Float32, reflect.Float64:
		g.code.lea(_R12, offset, _RBX)
		g.code.mov(_R10, _RAX)
		g.code.movConst(int64(typ.Bits()/8), _RCX)
		g.emitHelper(3)
	case reflect.String:
		g.code.loadMem(_R12, offset, _RBX)
		g.code.loadMem(_R12, offset+8, _RCX)
		g.code.mov(_R10, _RAX)
		g.emitHelper(1)
	case reflect.Pointer:
		return fmt.Errorf("json: JIT encoder does not support pointer field type %s", typ)
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
		return fmt.Errorf("json: JIT encoder does not support slice type %s", typ)
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
	if err := g.emitRaw([]byte("{")); err != nil {
		return err
	}
	for index, field := range fields {
		fieldOffset := offset + int64(typ.Field(field.index).Offset)
		if field.omitEmpty {
			return fmt.Errorf("json: JIT encoder does not support omitempty on %s", field.name)
		}
		if index > 0 {
			if err := g.emitRaw([]byte(",")); err != nil {
				return err
			}
		}
		key := append(appendQuoted(nil, field.name), ':')
		if err := g.emitRaw(key); err != nil {
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

func (g *jitEncoderGenerator) emitSlice(typ reflect.Type, offset int64) error {
	nilLabel := g.code.newLabel()
	endLabel := g.code.newLabel()
	g.code.loadMem(_R12, offset, _R11)
	g.code.test(_R11, _R11)
	g.code.jumpCond(x86.AJEQ, nilLabel)

	g.code.push(_R13)
	g.code.push(_R9)
	g.code.push(_R15)
	g.code.mov(_R11, _R9)
	g.code.loadMem(_R12, offset+8, _R13)
	g.code.xor(_R15, _R15)

	if typ.Elem().Kind() == reflect.Uint8 {
		g.code.mov(_R10, _RAX)
		g.code.mov(_R9, _RBX)
		g.code.mov(_R13, _RCX)
		g.emitHelper(7)
		g.code.pop(_R15)
		g.code.pop(_R9)
		g.code.pop(_R13)
		g.code.jump(endLabel)
		g.code.bind(nilLabel)
		g.code.mov(_R10, _RAX)
		g.emitHelper(5)
		g.code.bind(endLabel)
		return nil
	}

	loopLabel := g.code.newLabel()
	arrayEnd := g.code.newLabel()
	if err := g.emitRaw([]byte("[")); err != nil {
		return err
	}
	g.code.bind(loopLabel)
	g.code.test(_R13, _R13)
	g.code.jumpCond(x86.AJEQ, arrayEnd)
	noComma := g.code.newLabel()
	g.code.test(_R15, _R15)
	g.code.jumpCond(x86.AJEQ, noComma)
	if err := g.emitRaw([]byte(",")); err != nil {
		return err
	}
	g.code.bind(noComma)
	g.code.push(_R12)
	g.code.mov(_R9, _R12)
	g.code.setData(_R12)
	if err := g.emitValue(typ.Elem(), 0); err != nil {
		return err
	}
	g.code.pop(_R12)
	g.code.addConst(int64(typ.Elem().Size()), _R9)
	g.code.dec(_R13)
	g.code.inc(_R15)
	g.code.jump(loopLabel)
	g.code.bind(arrayEnd)
	if err := g.emitRaw([]byte("]")); err != nil {
		return err
	}
	g.code.pop(_R15)
	g.code.pop(_R9)
	g.code.pop(_R13)
	g.code.jump(endLabel)
	g.code.bind(nilLabel)
	g.code.mov(_R10, _RAX)
	g.emitHelper(5)
	g.code.bind(endLabel)
	return nil
}

func (g *jitEncoderGenerator) emitRaw(value []byte) error {
	if len(value) == 0 {
		return nil
	}
	offset := len(g.static)
	g.static = append(g.static, value...)
	g.code.loadMem(_R10, int64(unsafe.Offsetof(jitEncodeContext{}.program)), _R11)
	g.code.loadMem(_R11, int64(unsafe.Offsetof(jitEncodeProgram{}.static)), _RBX)
	g.code.addConst(int64(offset), _RBX)
	g.code.mov(_R10, _RAX)
	g.code.movConst(int64(len(value)), _RCX)
	g.emitHelper(0)
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

func (g *jitEncoderGenerator) emitHelper0(index int) {
	g.code.mov(_R10, _RAX)
	g.emitHelper(index)
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

func jitEmptyKind(typ reflect.Type) int {
	switch typ.Kind() {
	case reflect.String:
		return jitEmptyString
	case reflect.Bool:
		return jitEmptyBool
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return jitEmptyInt
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return jitEmptyUint
	case reflect.Float32, reflect.Float64:
		return jitEmptyFloat
	case reflect.Pointer:
		return jitEmptyPointer
	case reflect.Slice, reflect.Map:
		return jitEmptySlice
	default:
		return -1
	}
}

type jitCodeBuilder struct {
	name     string
	builder  *golangasm.Builder
	pc       int
	progs    []*obj.Prog
	labels   []*jitLabel
	branches []jitBranch
}

type jitLabel struct {
	pos int
}

type jitBranch struct {
	start  *obj.Prog
	label  *jitLabel
	dispAt int
	size   int
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
	c.progs = append(c.progs, prog)
	c.pc++
}

func (c *jitCodeBuilder) mov(from, to obj.Addr) {
	c.emit(&obj.Prog{As: x86.AMOVQ, From: from, To: to})
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

func (c *jitCodeBuilder) leaIndexed(base obj.Addr, offset int64, index obj.Addr, scale int64, to obj.Addr) {
	c.emit(&obj.Prog{
		As: x86.ALEAQ,
		From: obj.Addr{
			Type:   obj.TYPE_MEM,
			Reg:    base.Reg,
			Index:  index.Reg,
			Scale:  int16(scale),
			Offset: offset,
		},
		To: to,
	})
}

func (c *jitCodeBuilder) add(from, to obj.Addr) {
	c.emit(&obj.Prog{As: x86.AADDQ, From: from, To: to})
}

func (c *jitCodeBuilder) addConst(value int64, to obj.Addr) {
	c.emit(&obj.Prog{As: x86.AADDQ, From: obj.Addr{Type: obj.TYPE_CONST, Offset: value}, To: to})
}

func (c *jitCodeBuilder) subConst(value int64, from obj.Addr) {
	c.emit(&obj.Prog{As: x86.ASUBQ, From: obj.Addr{Type: obj.TYPE_CONST, Offset: value}, To: from})
}

func (c *jitCodeBuilder) inc(reg obj.Addr) {
	c.emit(&obj.Prog{As: x86.AINCQ, From: reg, To: reg})
}

func (c *jitCodeBuilder) dec(reg obj.Addr) {
	c.emit(&obj.Prog{As: x86.ADECQ, From: reg, To: reg})
}

func (c *jitCodeBuilder) xor(from, to obj.Addr) {
	c.emit(&obj.Prog{As: x86.AXORQ, From: from, To: to})
}

func (c *jitCodeBuilder) test(from, to obj.Addr) {
	c.emit(&obj.Prog{As: x86.ATESTQ, From: from, To: to})
}

func (c *jitCodeBuilder) push(reg obj.Addr) {
	c.loadMem(_RSP, 0, _R10)
	c.loadMem(_R10, int64(unsafe.Offsetof(jitEncodeContext{}.slotTop)), _R11)
	c.leaIndexed(_R10, int64(unsafe.Offsetof(jitEncodeContext{}.slots)), _R11, 8, _R8)
	c.mov(reg, mem(_R8, 0))
	c.inc(_R11)
	c.mov(_R11, mem(_R10, int64(unsafe.Offsetof(jitEncodeContext{}.slotTop))))
	if reg.Reg == _R12.Reg {
		c.mov(reg, mem(_R10, int64(unsafe.Offsetof(jitEncodeContext{}.data))))
	}
}

func (c *jitCodeBuilder) pop(reg obj.Addr) {
	c.loadMem(_RSP, 0, _R10)
	c.loadMem(_R10, int64(unsafe.Offsetof(jitEncodeContext{}.slotTop)), _R11)
	c.dec(_R11)
	c.mov(_R11, mem(_R10, int64(unsafe.Offsetof(jitEncodeContext{}.slotTop))))
	c.leaIndexed(_R10, int64(unsafe.Offsetof(jitEncodeContext{}.slots)), _R11, 8, _R8)
	c.mov(mem(_R8, 0), reg)
	if reg.Reg == _R12.Reg {
		c.mov(reg, mem(_R10, int64(unsafe.Offsetof(jitEncodeContext{}.data))))
	}
}

func (c *jitCodeBuilder) setData(reg obj.Addr) {
	c.loadMem(_RSP, 0, _R10)
	c.mov(reg, mem(_R10, int64(unsafe.Offsetof(jitEncodeContext{}.data))))
}

func (c *jitCodeBuilder) saveJITRegs() {
	c.mov(_R12, mem(_R10, int64(unsafe.Offsetof(jitEncodeContext{}.saved12))))
	c.mov(_R13, mem(_R10, int64(unsafe.Offsetof(jitEncodeContext{}.saved13))))
	c.mov(_R9, mem(_R10, int64(unsafe.Offsetof(jitEncodeContext{}.saved9))))
	c.mov(_R15, mem(_R10, int64(unsafe.Offsetof(jitEncodeContext{}.saved15))))
}

func (c *jitCodeBuilder) restoreJITRegs() {
	c.loadMem(_RSP, 0, _R10)
	c.loadMem(_R10, int64(unsafe.Offsetof(jitEncodeContext{}.data)), _R12)
	c.loadMem(_R10, int64(unsafe.Offsetof(jitEncodeContext{}.saved13)), _R13)
	c.loadMem(_R10, int64(unsafe.Offsetof(jitEncodeContext{}.saved9)), _R9)
	c.loadMem(_R10, int64(unsafe.Offsetof(jitEncodeContext{}.saved15)), _R15)
}

func (c *jitCodeBuilder) call(reg obj.Addr) {
	c.emit(&obj.Prog{As: obj.ACALL, To: reg})
}

func (c *jitCodeBuilder) ret() {
	c.emit(&obj.Prog{As: obj.ARET})
}

func (c *jitCodeBuilder) newLabel() *jitLabel {
	label := &jitLabel{pos: -1}
	c.labels = append(c.labels, label)
	return label
}

func (c *jitCodeBuilder) bind(label *jitLabel) {
	if label.pos >= 0 {
		panic("JIT label bound more than once")
	}
	label.pos = len(c.progs)
}

func (c *jitCodeBuilder) jump(label *jitLabel) {
	c.emitBranch(obj.AJMP, label)
}

func (c *jitCodeBuilder) jumpCond(condition obj.As, label *jitLabel) {
	c.emitBranch(condition, label)
}

func (c *jitCodeBuilder) assemble() ([]byte, error) {
	code := c.builder.Assemble()
	for _, branch := range c.branches {
		if branch.label.pos < 0 {
			return nil, errors.New("JIT label was not bound")
		}
		target := len(code)
		if branch.label.pos < len(c.progs) {
			target = int(c.progs[branch.label.pos].Pc)
		}
		start := int(branch.start.Pc)
		disp := target - (start + branch.size)
		binary.LittleEndian.PutUint32(code[start+branch.dispAt:start+branch.dispAt+4], uint32(int32(disp)))
	}
	return code, nil
}

func (c *jitCodeBuilder) emitBranch(condition obj.As, label *jitLabel) {
	start := c.builder.NewProg()
	if condition == obj.AJMP {
		start.As = x86.ABYTE
		start.From = constAddr(0xe9)
		c.emit(start)
		disp := c.builder.NewProg()
		disp.As = x86.ALONG
		disp.From = constAddr(0)
		c.emit(disp)
		c.branches = append(c.branches, jitBranch{start: start, label: label, dispAt: 1, size: 5})
		return
	}
	opcode, ok := jitConditionOpcode(condition)
	if !ok {
		panic(fmt.Sprintf("unsupported JIT branch condition: %v", condition))
	}
	start.As = x86.ABYTE
	start.From = constAddr(0x0f)
	c.emit(start)
	opcodeProg := c.builder.NewProg()
	opcodeProg.As = x86.ABYTE
	opcodeProg.From = constAddr(int64(opcode))
	c.emit(opcodeProg)
	disp := c.builder.NewProg()
	disp.As = x86.ALONG
	disp.From = constAddr(0)
	c.emit(disp)
	c.branches = append(c.branches, jitBranch{start: start, label: label, dispAt: 2, size: 6})
}

func jitConditionOpcode(condition obj.As) (byte, bool) {
	switch condition {
	case x86.AJEQ:
		return 0x84, true
	case x86.AJNE:
		return 0x85, true
	case x86.AJGE:
		return 0x8d, true
	case x86.AJGT:
		return 0x8f, true
	case x86.AJLT:
		return 0x8c, true
	case x86.AJLE:
		return 0x8e, true
	default:
		return 0, false
	}
}

func constAddr(value int64) obj.Addr {
	return obj.Addr{Type: obj.TYPE_CONST, Offset: value}
}

func mem(base obj.Addr, offset int64) obj.Addr {
	return obj.Addr{Type: obj.TYPE_MEM, Reg: base.Reg, Offset: offset}
}

var (
	_RAX = obj.Addr{Type: obj.TYPE_REG, Reg: x86.REG_AX}
	_RBX = obj.Addr{Type: obj.TYPE_REG, Reg: x86.REG_BX}
	_RCX = obj.Addr{Type: obj.TYPE_REG, Reg: x86.REG_CX}
	_R8  = obj.Addr{Type: obj.TYPE_REG, Reg: x86.REG_R8}
	_R9  = obj.Addr{Type: obj.TYPE_REG, Reg: x86.REG_R9}
	_R10 = obj.Addr{Type: obj.TYPE_REG, Reg: x86.REG_R10}
	_R11 = obj.Addr{Type: obj.TYPE_REG, Reg: x86.REG_R11}
	_R12 = obj.Addr{Type: obj.TYPE_REG, Reg: x86.REG_R12}
	_R13 = obj.Addr{Type: obj.TYPE_REG, Reg: x86.REG_R13}
	_R15 = obj.Addr{Type: obj.TYPE_REG, Reg: x86.REG_R15}
	_RDI = obj.Addr{Type: obj.TYPE_REG, Reg: x86.REG_DI}
	_RSI = obj.Addr{Type: obj.TYPE_REG, Reg: x86.REG_SI}
	_RSP = obj.Addr{Type: obj.TYPE_REG, Reg: x86.REG_SP}
)
