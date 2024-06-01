package compiler

import (
	"github.com/twitchyliquid64/golang-asm/obj/arm64"
)

var GoAbiArg1 = obj.Addr{Type: obj.TYPE_REG, Reg: arm64.REG_R8}
var GoAbiArg2 = obj.Addr{Type: obj.TYPE_REG, Reg: x86.REG_BX}
var GoAbiArg3 = obj.Addr{Type: obj.TYPE_REG, Reg: x86.REG_CX}
var GoAbiArg4 = obj.Addr{Type: obj.TYPE_REG, Reg: x86.REG_DI}
var GoAbiArg5 = obj.Addr{Type: obj.TYPE_REG, Reg: x86.REG_SI}

var GoStackReg = obj.Addr{Type: obj.TYPE_REG, Reg: x86.REG_SP}
var FramePointerReg = obj.Addr{Type: obj.TYPE_REG, Reg: x86.REG_BP}
var JitStackReg = obj.Addr{Type: obj.TYPE_REG, Reg: x86.REG_R8}
var BufferPtrReg = obj.Addr{Type: obj.TYPE_REG, Reg: x86.REG_R9}
var CusPtrReg = obj.Addr{Type: obj.TYPE_REG, Reg: x86.REG_R10}
var JitTempReg = obj.Addr{Type: obj.TYPE_CONST, Reg: x86.REG_R11}
