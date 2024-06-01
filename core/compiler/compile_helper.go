package compiler

import (
	"github.com/twitchyliquid64/golang-asm/obj"
	"github.com/twitchyliquid64/golang-asm/obj/x86"
	"reflect"
)

func handlePointer(set *InstSet, typ reflect.Type, offset uint32) reflect.Type {
	var ptrDepth int
	for typ.Kind() == reflect.Pointer {
		ptrDepth++
		typ = typ.Elem()
	}
	if ptrDepth > 1 {
		set.Append(&Instruction{
			Def: InsDeRef,
			Arg: uint64(offset) << 32,
		})
	}
	if ptrDepth > 2 {
		for i := 2; i < ptrDepth; i++ {
			set.Append(&Instruction{
				Def: InsDeRef,
				Arg: 0,
			})
		}
	}
	return typ
}

func deRef(typ reflect.Type) reflect.Type {
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	return typ
}

func setEncoderPrologue(p *Program) {
	p.AddProgram(&obj.Prog{
		As:   x86.AMOVQ,
		From: GoAbiArg1,
		To:   JitStackReg,
	})
	p.AddProgram(&obj.Prog{
		As:   x86.AMOVQ,
		From: GoAbiArg2,
		To:   BufferPtrReg,
	})
	p.AddProgram(&obj.Prog{
		As:   x86.AMOVQ,
		From: GoAbiArg3,
		To:   CusPtrReg,
	})
}

func setEncoderEnd(p *Program) {
	p.AddProgram(&obj.Prog{
		As: obj.ARET,
	})
	p.SetLabelStart("jit_grow_slice", []*obj.Prog{
		{
			As: obj.AJMP,
			To: _GetDefFunc2Addr("defJitGrowSlice"),
		},
		{
			As: obj.ARET,
		},
	}...)
}
