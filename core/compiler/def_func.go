package compiler

import (
	"encoding/hex"
	"fmt"
	"github.com/nyan233/jjs-go/core/iasm"
	"github.com/nyan233/jjs-go/core/mempool"
	"github.com/twitchyliquid64/golang-asm/obj"
	"github.com/twitchyliquid64/golang-asm/obj/x86"
	"unsafe"
)

var _DefFuncTable = make(map[string]*uintptr)

func _RegisterDefFunc(name string, codeBuf []byte) {
	s1 := mempool.MallocSlice[byte](len(codeBuf), len(codeBuf))
	copy(s1, codeBuf)
	ptr := mempool.ArenaNew[uintptr]()
	*ptr = uintptr(unsafe.Pointer(&s1[0]))
	if _, ok := _DefFuncTable[name]; ok {
		panic(fmt.Sprintf("dup func name : %s", name))
	}
	_DefFuncTable[name] = ptr
}

func _GetDefFunc2Addr(name string) obj.Addr {
	codePtr, ok := _DefFuncTable[name]
	if !ok {
		panic(fmt.Sprintf("def func name is not found : %s", name))
	}
	return obj.Addr{
		Type:   obj.TYPE_MEM,
		Offset: int64(uintptr(unsafe.Pointer(codePtr))),
	}
}

/*
		mov 8(R9), DI // slice new len -> DI
		mov DI, BX // slice new len -> BX
		mov 16(R9), CX // slice cap -> CX
		sub BX, DI // num -> DI
		mov 0(R9), AX // slice ptr -> AX
		mov $bytesETypePtr, SI
		jit_all_reg_save
		call runtime·growslice
		jit_all_reg_recover
		mov AX, 0(R9) // AX -> slice ptr
	    mov BX, 8(R9) // BX -> slice len
	    mov CX, 16(R9) // CX -> slice cap
		jit_pop R11 // jit_stack -> r11
	    jmp R11
*/
func buildJitGrowSlice() (string, []byte) {
	p := newProgram()
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
		As:   x86.AMOVQ,
		From: GoAbiArg4,
		To:   GoAbiArg2,
	})
	p.AddProgram(&obj.Prog{
		As: x86.AMOVQ,
		From: obj.Addr{
			Type:   obj.TYPE_MEM,
			Reg:    BufferPtrReg.Reg,
			Offset: 16,
		},
		To: GoAbiArg3,
	})
	p.AddProgram(&obj.Prog{
		As:   x86.ASUBQ,
		From: GoAbiArg2,
		To:   GoAbiArg4,
	})
	p.AddProgram(&obj.Prog{
		As: x86.AMOVQ,
		From: obj.Addr{
			Type:   obj.TYPE_MEM,
			Reg:    BufferPtrReg.Reg,
			Offset: 0,
		},
		To: GoAbiArg1,
	})
	p.AddProgram(&obj.Prog{
		As: x86.AMOVQ,
		From: obj.Addr{
			Type:   obj.TYPE_CONST,
			Offset: int64(uintptr(iasm.BytesEtype)),
		},
		To: GoAbiArg5,
	})
	p.CallGO(iasm.GetJitFunc2Addr("growslice"))
	p.AddProgram(&obj.Prog{
		As:   x86.AMOVQ,
		From: GoAbiArg1,
		To: obj.Addr{
			Type:   obj.TYPE_MEM,
			Reg:    BufferPtrReg.Reg,
			Offset: 0,
		},
	})
	p.AddProgram(&obj.Prog{
		As:   x86.AMOVQ,
		From: GoAbiArg2,
		To: obj.Addr{
			Type:   obj.TYPE_MEM,
			Reg:    BufferPtrReg.Reg,
			Offset: 8,
		},
	})
	p.AddProgram(&obj.Prog{
		As:   x86.AMOVQ,
		From: GoAbiArg3,
		To: obj.Addr{
			Type:   obj.TYPE_MEM,
			Reg:    BufferPtrReg.Reg,
			Offset: 16,
		},
	})
	p.JPop(JitTempReg, 0)
	p.AddProgram(&obj.Prog{
		As: obj.AJMP,
		To: JitTempReg,
	})
	codeBuf, err := p.Build()
	if err != nil {
		panic(err)
	}
	if Debug {
		logger.Debug(hex.EncodeToString(codeBuf))
	}
	return "defJitGrowSlice", codeBuf
}

func init() {
	_RegisterDefFunc(buildJitGrowSlice())
}
