package compiler

import (
	"encoding/hex"
	"fmt"
	"reflect"
	"sync"
	"unsafe"
)

var gEncoder = new(Encoder).init()

func CompileEncoder(typ reflect.Type, opt uint64) (EncodeHandler, error) {
	return gEncoder.Compile(typ, opt)
}

type Encoder struct {
	baseCache
	mu sync.Mutex
}

func (e *Encoder) init() *Encoder {
	e.baseCache.init()
	return e
}

func (e *Encoder) Compile(typ reflect.Type, opt uint64) (EncodeHandler, error) {
	function, ok := e.getCompileResult(typ)
	if ok {
		return *(*EncodeHandler)(unsafe.Pointer(&function)), nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	set := InstSetPool.Get().(*InstSet)
	defer func() {
		set.Reset()
		InstSetPool.Put(set)
	}()
	err := e.doCompile(set, typ, 0, opt)
	if err != nil {
		return nil, err
	}
	p := newProgram()
	setEncoderPrologue(p)
	CompileInstruction(p, set)
	setEncoderEnd(p)
	code, err := p.Build()
	if err != nil {
		return nil, err
	}
	if Debug {
		logger.Debug(hex.EncodeToString(code))
	}
	function2 := jitLoader.LoadOne(code, "jjs_encoder", 0, 32, []bool{false, false, true, false}, []bool{})
	if Debug {
		logger.Debug(fmt.Sprintf("type(%s) Code : S(0x%x) -> E(0x%x)",
			typ.Name(), *(*uintptr)(function2), *(*uintptr)(function2)+uintptr(len(code))))
	}
	e.setCompileResult(typ, unsafe.Pointer(function2))
	return *(*EncodeHandler)(unsafe.Pointer(&function2)), nil
}

func (e *Encoder) doCompile(set *InstSet, typ reflect.Type, offset uint32, opt uint64) error {
	typ = handlePointer(set, typ, offset)
	switch typ.Kind() {
	case reflect.Struct:
		e.compileStruct(set, typ, offset)
		break
	case reflect.Map:
		e.compileMap(set, typ, offset)
		break
	case reflect.Slice:
		e.compileSlice(set, typ, offset)
		break
	case reflect.Array:
		e.compileArray(set, typ, offset)
		break
	case reflect.String:
		set.Append(&Instruction{
			Def:    InsWString,
			Arg:    0,
			Detail: `"`,
		})
		set.Append(&Instruction{
			Def:    InsWString,
			Arg:    uint64(offset) << 32,
			Detail: nil,
		})
		set.Append(&Instruction{
			Def:    InsWString,
			Arg:    0,
			Detail: `"`,
		})
	case reflect.Bool:
		set.Append(&Instruction{
			Def: InsWBool,
			Arg: uint64(offset) << 32,
		})
	case reflect.Int, reflect.Int64, reflect.Uint, reflect.Uint64, reflect.Float64:
		set.Append(&Instruction{
			Def: InsWNumber,
			Arg: (uint64(offset) << 32) | 8,
		})
	case reflect.Int32, reflect.Uint32, reflect.Float32:
		set.Append(&Instruction{
			Def: InsWNumber,
			Arg: (uint64(offset) << 32) | 4,
		})
	case reflect.Int16, reflect.Uint16:
		set.Append(&Instruction{
			Def: InsWNumber,
			Arg: (uint64(offset) << 32) | 2,
		})
	case reflect.Int8, reflect.Uint8:
		set.Append(&Instruction{
			Def: InsWNumber,
			Arg: (uint64(offset) << 32) | 1,
		})
	}
	return nil
}

func (e *Encoder) compileStruct(set *InstSet, typ reflect.Type, offset uint32) {
	set.Append(&Instruction{
		Def:    InsStructEncStart,
		Arg:    0,
		Detail: nil,
	})
	for i := 0; i < typ.NumField(); i++ {
		e.compileStructField(set, i, typ.Field(i))
		if i < typ.NumField()-1 {
			set.Append(&Instruction{
				Def: InsStructNextField,
				Arg: 0,
			})
		}
	}
	set.Append(&Instruction{
		Def:    InsStructEncEnd,
		Arg:    0,
		Detail: nil,
	})
}

func (e *Encoder) compileStructField(set *InstSet, index int, f reflect.StructField) {
	set.Append(&Instruction{
		Def:    InsWString,
		Detail: fmt.Sprintf(`"%s":`, f.Name),
	})
	_ = e.doCompile(set, f.Type, uint32(f.Offset), 0)
}

func (e *Encoder) compileMap(set *InstSet, typ reflect.Type, offset uint32) {
	set.Append(&Instruction{
		Def:    InsMapEncStart,
		Arg:    0,
		Detail: nil,
	})
	set.Append(&Instruction{
		Def:    InsMapEncRange,
		Arg:    0,
		Detail: nil,
	})
	set.Append(&Instruction{
		Def:    InsMapWKey,
		Arg:    0,
		Detail: typ.Key(),
	})
	set.Append(&Instruction{
		Def:    InsMapNextKey,
		Arg:    0,
		Detail: nil,
	})
	set.Append(&Instruction{
		Def:    InsMapEncRangeEnd,
		Arg:    0,
		Detail: nil,
	})
	set.Append(&Instruction{
		Def:    InsMapEncEnd,
		Arg:    0,
		Detail: nil,
	})
}

func (e *Encoder) compileSlice(set *InstSet, typ reflect.Type, offset uint32) {
	set.Append(&Instruction{
		Def:    InsSlice2Array,
		Arg:    uint64(offset) << 32,
		Detail: nil,
	})
	e.compileArray(set, typ, 0)
}

func (e *Encoder) compileArray(set *InstSet, typ reflect.Type, offset uint32) {
	set.Append(&Instruction{
		Def:    InsArrayEncStart,
		Arg:    0,
		Detail: nil,
	})
	set.Append(&Instruction{
		Def:    InsArrayWElem,
		Arg:    0,
		Detail: nil,
	})
	_ = e.doCompile(set, typ.Elem(), 0, 0)
	set.Append(&Instruction{
		Def:    InsArrayNext,
		Arg:    0,
		Detail: nil,
	})
	set.Append(&Instruction{
		Def:    InsArrayEncEnd,
		Arg:    0,
		Detail: nil,
	})
}
