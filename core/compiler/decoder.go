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

var jitDecodeContexts sync.Map

func CompileDecoder(typ reflect.Type) (DecodeHandler, error) {
	return gDecoder.Compile(typ)
}

type Decoder struct {
	baseCache[DecodeHandler]
	mu sync.Mutex
}

func (d *Decoder) init() *Decoder {
	d.baseCache.init()
	return d
}

func (d *Decoder) Compile(typ reflect.Type) (DecodeHandler, error) {
	if typ == nil {
		return nil, errors.New("json: cannot compile a nil type")
	}
	if handler, ok := d.getCompileResult(typ); ok {
		return handler, nil
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if handler, ok := d.getCompileResult(typ); ok {
		return handler, nil
	}
	program, err := compileJITDecoder(typ)
	if err != nil {
		return nil, err
	}
	handler := func(data []byte, target unsafe.Pointer) error {
		if target == nil {
			return errors.New("json: nil decode destination")
		}
		root, err := parseJITJSON(data)
		if err != nil {
			return err
		}
		ctx := &jitDecodeContext{
			root:    root,
			data:    target,
			program: program.program,
		}
		jitDecodeContexts.Store(ctx, struct{}{})
		defer jitDecodeContexts.Delete(ctx)
		program.function(ctx)
		return ctx.err
	}
	d.setCompileResult(typ, handler)
	return handler, nil
}

type jitDecodeProgram struct {
	helpers [4]uintptr
	static  []byte
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
}

func newJITDecodeProgram(static []byte) *jitDecodeProgram {
	return &jitDecodeProgram{
		helpers: [4]uintptr{
			reflect.ValueOf(jitDecodeString).Pointer(),
			reflect.ValueOf(jitDecodeInteger).Pointer(),
			reflect.ValueOf(jitDecodeFloat).Pointer(),
			reflect.ValueOf(jitDecodeBool).Pointer(),
		},
		static: static,
	}
}

func compileJITDecoder(typ reflect.Type) (*jitCompiledDecoder, error) {
	generator := &jitDecoderGenerator{code: newJITCodeBuilder("jjs_decoder")}
	if err := generator.emitPrologue(); err != nil {
		return nil, err
	}
	if err := generator.emitValue(typ, 0, nil); err != nil {
		return nil, err
	}
	g := generator
	g.code.addConst(64, _RSP)
	g.code.xor(_RAX, _RAX)
	g.code.ret()
	code, err := g.code.assemble()
	if err != nil {
		return nil, err
	}
	program := newJITDecodeProgram(generator.static)
	libFunc := jitLoader.LoadOne(code, "jjs_decoder", 64, 16, []bool{true, false}, []bool{true, false, false, false, false, false, false, false})
	function := *(*func(*jitDecodeContext) uintptr)(unsafe.Pointer(&libFunc))
	return &jitCompiledDecoder{program: program, function: function}, nil
}

type jitDecoderGenerator struct {
	code   *jitCodeBuilder
	static []byte
}

func (g *jitDecoderGenerator) emitPrologue() error {
	g.code.subConst(64, _RSP)
	g.code.mov(_RAX, mem(_RSP, 0))
	g.code.mov(_RAX, _R10)
	g.code.loadMem(_R10, int64(unsafe.Offsetof(jitDecodeContext{}.data)), _R12)
	return nil
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

func (g *jitDecoderGenerator) emitStruct(typ reflect.Type, offset int64, path []jitPathSegment) error {
	fields, err := jitExportedFields(typ)
	if err != nil {
		return err
	}
	for _, field := range fields {
		childPath := append(append([]jitPathSegment(nil), path...), jitPathSegment{field: field.name})
		if err := g.emitValue(field.typ, offset+int64(typ.Field(field.index).Offset), childPath); err != nil {
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
	g.emitHelper(helper)
	return nil
}

func (g *jitDecoderGenerator) addPath(path []jitPathSegment) (int, int) {
	offset := len(g.static)
	for _, segment := range path {
		if segment.array {
			g.static = append(g.static, 1, 4, 0)
			payload := len(g.static)
			g.static = append(g.static, 0, 0, 0, 0)
			binary.LittleEndian.PutUint32(g.static[payload:payload+4], uint32(segment.index))
			continue
		}
		if len(segment.field) > 0xffff {
			panic("JSON field name is too long")
		}
		g.static = append(g.static, 0, byte(len(segment.field)), byte(len(segment.field)>>8))
		g.static = append(g.static, segment.field...)
	}
	return offset, len(g.static) - offset
}

func (g *jitDecoderGenerator) emitHelper(index int) {
	g.code.loadMem(_RSP, 0, _R11)
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
	c.loadMem(_RSP, 0, _R10)
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
