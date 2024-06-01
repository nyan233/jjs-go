package compiler

import (
	"fmt"
	"github.com/nyan233/jjs-go/core/helper"
	"github.com/nyan233/jjs-go/core/iasm"
	"github.com/nyan233/jjs-go/core/mempool"
	"github.com/twitchyliquid64/golang-asm/obj"
	"github.com/twitchyliquid64/golang-asm/obj/x86"
	"reflect"
	"unsafe"
)

var _InstCompiler = new(InstCompiler)

var HandlerTable = map[InstructionDef]func(_ *Program, _ *Instruction){
	InsDeRef:           _InstCompiler.handleDeRef,
	InsWString:         _InstCompiler.handleWString,
	InsWNumber:         _InstCompiler.handleWNumber,
	InsSlice2Array:     _InstCompiler.handleSlice2Array,
	InsArrayEncStart:   _InstCompiler.handleArrayEncStart,
	InsArrayWElem:      _InstCompiler.handleArrayWElem,
	InsArrayNext:       _InstCompiler.handleArrayNext,
	InsArrayEncEnd:     _InstCompiler.handleArrayEncEnd,
	InsStructEncStart:  _InstCompiler.handleStructEncStart,
	InsStructNextField: _InstCompiler.handleStructNextField,
	InsStructWField:    _InstCompiler.handleStructWField,
	InsStructEncEnd:    _InstCompiler.handleStructEncEnd,
	InsWBool:           _InstCompiler.handleWBool,
}

type InstCompiler struct {
}

func (i *InstCompiler) handleDeRef(p *Program, ins *Instruction) {
	argL := ParseUList[uint32](ins.Arg)
	offset := argL[0]
	pg := &obj.Prog{
		As: x86.AMOVQ,
		From: obj.Addr{
			Type:   obj.TYPE_MEM,
			Reg:    CusPtrReg.Reg,
			Offset: int64(offset),
		},
		To: CusPtrReg,
	}
	p.AddProgram(pg)
}

func (i *InstCompiler) handleStructEncStart(p *Program, ins *Instruction) {
	p.JPush(CusPtrReg, 0)
	argL := ParseUList[uint32](ins.Arg)
	offset := argL[0]
	p.AddProgram(&obj.Prog{
		As: x86.ALEAQ,
		From: obj.Addr{
			Type:   obj.TYPE_MEM,
			Reg:    CusPtrReg.Reg,
			Offset: int64(offset),
		},
		To: CusPtrReg,
	})
	i.writeConstBytes(p, []byte("{"))
}

func (i *InstCompiler) handleStructNextField(p *Program, ins *Instruction) {
	i.handleWString(p, &Instruction{
		Def:    InsWString,
		Arg:    ins.Arg,
		Detail: ",",
	})
}

func (i *InstCompiler) handleStructWField(p *Program, ins *Instruction) {
	typ := ins.Detail.(reflect.Type)
	i.handleValueType(p, ins.Arg, typ)
}

func (i *InstCompiler) handleValueType(p *Program, arg uint64, typ reflect.Type) {
	switch typ.Kind() {
	case reflect.String:
		i.handleWString(p, &Instruction{
			Def:    InsWString,
			Arg:    arg,
			Detail: nil,
		})
	case reflect.Bool:
		i.handleWBool(p, &Instruction{
			Def: InsWString,
			Arg: arg,
		})
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		break
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		break
	case reflect.Float32, reflect.Float64:
		break
	case reflect.Map:
		break
	case reflect.Interface:
		break
	default:
		panic(fmt.Sprintf("unknown type %s", typ.Kind()))
	}
}

func (i *InstCompiler) handleStructEncEnd(p *Program, ins *Instruction) {
	p.JPop(CusPtrReg, 0)
	i.writeConstBytes(p, []byte("}"))
}

/*
movq  8(R9), DI  // slice len -> DI
movq  16(R9), SI // slice cap -> SI
cmpq  DI, SI
#save_rip_2_jit_stack
jgt   jit_grow_slice
*/
func (i *InstCompiler) checkAndGrowBuf(p *Program) {
	p.AddProgram(&obj.Prog{
		As: x86.AMOVQ,
		From: obj.Addr{
			Type:   obj.TYPE_MEM,
			Reg:    BufferPtrReg.Reg,
			Offset: 8,
		},
		To: GoAbiArg4,
	})
	p.AddProgram(&obj.Prog{
		As: x86.AMOVQ,
		From: obj.Addr{
			Type:   obj.TYPE_MEM,
			Reg:    BufferPtrReg.Reg,
			Offset: 16,
		},
		To: GoAbiArg5,
	})
	p.SaveRIP2JitStack(9)
	p.CMP(GoAbiArg4, GoAbiArg5)
	p.JumpOfCond(x86.AJGT, "jit_grow_slice")
	p.JitStackFree(8)
}

/*
jit_push 8(R9) // slice old len -> SP
jit_push wLen // if type != TYPE_CONST

mov wLen, CX // if type == TYPE_MEM
add CX, 8(R9) // if type == TYPE_MEM

add wLen, 8(R9) // if type != TYPE_MEM
checkAndGrowBuf
jit_pop CX // wLen -> CX
jit_pop BX // old slice len -> BX
mov (R9), DI
lea (BX*1)(DI), AX
xor DI, DI
mov from, BX
callgo iasm.memmove
*/
func (i *InstCompiler) writeBytes2Buf(p *Program, from obj.Addr, wLen obj.Addr) {
	p.JPush(obj.Addr{
		Type:   obj.TYPE_MEM,
		Reg:    BufferPtrReg.Reg,
		Offset: 8,
	}, GoAbiArg1.Reg)
	if wLen.Type != obj.TYPE_CONST {
		p.JPush(wLen, GoAbiArg1.Reg)
	}
	var addFrom obj.Addr
	if wLen.Type == obj.TYPE_MEM {
		p.AddProgram(&obj.Prog{
			As:   x86.AMOVQ,
			From: wLen,
			To:   GoAbiArg3,
		})
		addFrom = GoAbiArg3
	} else {
		addFrom = wLen
	}
	p.AddProgram(&obj.Prog{
		As:   x86.AADDQ,
		From: addFrom,
		To: obj.Addr{
			Type:   obj.TYPE_MEM,
			Reg:    BufferPtrReg.Reg,
			Offset: 8,
		},
	})
	i.checkAndGrowBuf(p)
	if wLen.Type != obj.TYPE_CONST {
		p.JPop(GoAbiArg3, 0)
	} else {
		// 可以确定长度的情况下
		p.AddProgram(&obj.Prog{
			As:   x86.AMOVQ,
			From: wLen,
			To:   GoAbiArg3,
		})
	}
	p.JPop(GoAbiArg2, 0)
	p.AddProgram(&obj.Prog{
		As: x86.AMOVQ,
		From: obj.Addr{
			Type:   obj.TYPE_MEM,
			Reg:    BufferPtrReg.Reg,
			Offset: 0,
		},
		To: GoAbiArg4,
	})
	p.AddProgram(&obj.Prog{
		As: x86.ALEAQ,
		From: obj.Addr{
			Type:   obj.TYPE_MEM,
			Reg:    GoAbiArg4.Reg,
			Index:  GoAbiArg2.Reg,
			Scale:  1,
			Offset: 0,
		},
		To: GoAbiArg1,
	})
	p.AddProgram(&obj.Prog{
		As:   x86.AXORQ,
		From: GoAbiArg4,
		To:   GoAbiArg4,
	})
	p.AddProgram(&obj.Prog{
		As:   x86.AMOVQ,
		From: from,
		To:   GoAbiArg2,
	})
	p.CallGO(iasm.GetJitFunc2Addr("memmove"))
}

/*
jit_push 8(R9) // slice old len -> Jit_Stack
add len(buf), 8(R9)
checkAndGrowBuf
mov (R9), DI // deptr
jit_pop AX // Jit_Stack.slice_old_len -> AX
loop mov buf, offset(AX*1)(DI)
*/
func (i *InstCompiler) writeConstBytes(p *Program, buf []byte) {
	p.JPush(BufferSliceLenAddr, 0)
	p.AddProgram(&obj.Prog{
		As: x86.AADDQ,
		From: obj.Addr{
			Type:   obj.TYPE_CONST,
			Offset: int64(len(buf)),
		},
		To: BufferSliceLenAddr,
	})
	i.checkAndGrowBuf(p)
	p.AddProgram(&obj.Prog{
		As:   x86.AMOVQ,
		From: BufferSlicePtrAddr,
		To:   GoAbiArg4,
	})
	p.JPop(GoAbiArg1, 0)
	var writeOffset int64
	for len(buf) > 0 {
		if len(buf)/4 > 0 {
			p.AddProgram(&obj.Prog{
				As: x86.AMOVQ,
				From: obj.Addr{
					Type:   obj.TYPE_CONST,
					Offset: helper.Bytes2Int64(buf[:4]),
				},
				To: obj.Addr{
					Type:   obj.TYPE_MEM,
					Reg:    GoAbiArg4.Reg,
					Index:  GoAbiArg1.Reg,
					Scale:  1,
					Offset: writeOffset,
				},
			})
			writeOffset += 4
			buf = buf[4:]
			continue
		} else if len(buf)/2 > 0 {
			p.AddProgram(&obj.Prog{
				As: x86.AMOVW,
				From: obj.Addr{
					Type:   obj.TYPE_CONST,
					Offset: helper.Bytes2Int64(buf[:2]),
				},
				To: obj.Addr{
					Type:   obj.TYPE_MEM,
					Reg:    GoAbiArg4.Reg,
					Index:  GoAbiArg1.Reg,
					Scale:  1,
					Offset: writeOffset,
				},
			})
			writeOffset += 2
			buf = buf[2:]
			continue
		} else if len(buf) > 0 {
			p.AddProgram(&obj.Prog{
				As: x86.AMOVB,
				From: obj.Addr{
					Type:   obj.TYPE_CONST,
					Offset: helper.Bytes2Int64(buf[:1]),
				},
				To: obj.Addr{
					Type:   obj.TYPE_MEM,
					Reg:    GoAbiArg4.Reg,
					Index:  GoAbiArg1.Reg,
					Scale:  1,
					Offset: writeOffset,
				},
			})
			writeOffset += 1
			buf = buf[1:]
			continue
		} else {
			panic(fmt.Sprintf("unknown buf len : %d", len(buf)))
		}
	}
}

func (i *InstCompiler) handleWString(p *Program, ins *Instruction) {
	// Dynamic string
	if ins.Detail == nil {
		argL := ParseUList[uint32](ins.Arg)
		offset := argL[0]
		i.writeBytes2Buf(p, obj.Addr{
			Type:   obj.TYPE_MEM,
			Reg:    CusPtrReg.Reg,
			Offset: int64(offset),
		}, obj.Addr{
			Type:   obj.TYPE_MEM,
			Reg:    CusPtrReg.Reg,
			Offset: int64(offset + 8),
		})
		return
	}
	valS, ok := ins.Detail.(string)
	if !ok {
		panic(fmt.Sprintf("detail type is not string, type=%s", reflect.TypeOf(ins.Detail).Kind()))
	}
	if len(valS) > 0 && len(valS) <= 8 {
		i.writeConstBytes(p, []byte(valS))
	} else {
		buf := mempool.MallocAlignSlice[byte](16, len(valS))
		ptr := uintptr(unsafe.Pointer(&buf[0]))
		p.MemAlloc(p.PC(), ptr)
		copy(buf, valS)
		i.writeBytes2Buf(p, obj.Addr{
			Type:   obj.TYPE_CONST,
			Offset: int64(ptr),
		}, obj.Addr{
			Type:   obj.TYPE_CONST,
			Offset: int64(len(valS)),
		})
	}
}

func (i *InstCompiler) handleWNumber(p *Program, ins *Instruction) {
	typ := ins.Detail.(reflect.Type)
	i.handleValueType(p, ins.Arg, typ)
}

func (i *InstCompiler) handleSlice2Array(p *Program, ins *Instruction) {

}

func (i *InstCompiler) handleArrayEncStart(p *Program, ins *Instruction) {

}

func (i *InstCompiler) handleArrayWElem(p *Program, ins *Instruction) {

}

func (i *InstCompiler) handleArrayNext(p *Program, ins *Instruction) {

}

func (i *InstCompiler) handleArrayEncEnd(p *Program, ins *Instruction) {

}

func (i *InstCompiler) handleWBool(p *Program, ins *Instruction) {
	//argL := ParseUList[uint32](ins.Arg)
	//offset := argL[0]
	//p.AddProgram(&obj.Prog{
	//	As: x86.AADDQ,
	//	From: obj.Addr{
	//		Type: obj.TYPE_CONST,
	//	},
	//})
	//i.checkAndGrowBuf(p)
}

func CompileInstruction(p *Program, set *InstSet) {
	for _, val := range *set {
		if fn, ok := HandlerTable[val.Def]; ok {
			p.AddJitInst(val)
			fn(p, val)
		} else {
			panic(fmt.Sprintf("unsupport ins(%d)", val.Def))
		}
	}
}
