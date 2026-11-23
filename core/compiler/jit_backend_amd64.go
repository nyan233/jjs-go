//go:build amd64 && !arm64_asm_test

package compiler

import (
	"github.com/nyan233/jjs-go/internal/jitloader"
	golangasm "github.com/twitchyliquid64/golang-asm"
	"github.com/twitchyliquid64/golang-asm/obj"
	"github.com/twitchyliquid64/golang-asm/obj/x86"
)

const (
	jitFrameSize = 64
	// Six helper argument spill slots occupy SP+0 through SP+40.
	jitFrameContext = 48
	jitFramePointer = 56
)

func (c *jitCodeBuilder) machineMov(from, to obj.Addr) {
	c.emit(&obj.Prog{As: x86.AMOVQ, From: from, To: to})
}

func (c *jitCodeBuilder) machineVectorMove32(from, to obj.Addr) {
	c.emit(&obj.Prog{As: x86.AVMOVDQU, From: from, To: _Y0})
	c.emit(&obj.Prog{As: x86.AVMOVDQU, From: _Y0, To: to})
}

func (c *jitCodeBuilder) machineVectorMove16(from, to obj.Addr) {
	c.emit(&obj.Prog{As: x86.AVMOVDQU, From: from, To: _X0})
	c.emit(&obj.Prog{As: x86.AVMOVDQU, From: _X0, To: to})
}

func (c *jitCodeBuilder) machineMovConst(value int64, to obj.Addr) {
	c.emit(&obj.Prog{As: x86.AMOVQ, From: obj.Addr{Type: obj.TYPE_CONST, Offset: value}, To: to})
}

func (c *jitCodeBuilder) machineLoadMem(base obj.Addr, offset int64, to obj.Addr) {
	c.machineMov(mem(base, offset), to)
}

func (c *jitCodeBuilder) machineLea(base obj.Addr, offset int64, to obj.Addr) {
	c.emit(&obj.Prog{As: x86.ALEAQ, From: mem(base, offset), To: to})
}

func (c *jitCodeBuilder) machineAddConst(value int64, to obj.Addr) {
	c.emit(&obj.Prog{As: x86.AADDQ, From: obj.Addr{Type: obj.TYPE_CONST, Offset: value}, To: to})
}

func (c *jitCodeBuilder) machineSubConst(value int64, from obj.Addr) {
	c.emit(&obj.Prog{As: x86.ASUBQ, From: obj.Addr{Type: obj.TYPE_CONST, Offset: value}, To: from})
}

func (c *jitCodeBuilder) machineXor(from, to obj.Addr) {
	c.emit(&obj.Prog{As: x86.AXORQ, From: from, To: to})
}

func (c *jitCodeBuilder) machineStore32(from, to obj.Addr) {
	c.emit(&obj.Prog{As: x86.AMOVL, From: from, To: to})
}

func (c *jitCodeBuilder) machineStore16(from, to obj.Addr) {
	c.emit(&obj.Prog{As: x86.AMOVW, From: from, To: to})
}

func (c *jitCodeBuilder) machineStore8(from, to obj.Addr) {
	c.emit(&obj.Prog{As: x86.AMOVB, From: from, To: to})
}

func (c *jitCodeBuilder) machineFunctionPrologue(frameSize int64) {
	c.machineSubConst(frameSize, _RSP)
	c.machineMov(_RBP, mem(_RSP, jitFramePointer))
	c.machineLea(_RSP, jitFramePointer, _RBP)
	c.machineMov(_RAX, mem(_RSP, jitFrameContext))
}

func (c *jitCodeBuilder) machineFinishFunction(frameSize int64) {
	c.machineLoadMem(_RSP, jitFramePointer, _RBP)
	c.machineAddConst(frameSize, _RSP)
	c.machineXor(_RAX, _RAX)
	c.machineRet()
}

func jitLocalPointers() []bool {
	return []bool{true}
}

func (c *jitCodeBuilder) machineCall(reg obj.Addr) {
	c.emit(&obj.Prog{As: obj.ACALL, To: reg})
}

func (c *jitCodeBuilder) machineRet() {
	c.emit(&obj.Prog{As: obj.ARET})
}

func (c *jitCodeBuilder) assemble() ([]byte, error) {
	instructions, stats, err := c.prepareMachineCode()
	if err != nil {
		return nil, err
	}
	builder, err := golangasm.NewBuilder("amd64", 64)
	if err != nil {
		return nil, err
	}
	for _, instruction := range instructions {
		builder.AddInstruction(instruction)
	}
	code := builder.Assemble()
	var stackDelta int32
	for index, instruction := range instructions {
		if instruction.To == _RSP && (instruction.As == x86.ASUBQ || instruction.As == x86.AADDQ) {
			c.pcsp = append(c.pcsp, jitloader.Pcvalue{PC: uint32(instructions[index+1].Pc), Val: stackDelta})
			if instruction.As == x86.ASUBQ {
				stackDelta += int32(instruction.From.Offset)
			} else {
				stackDelta -= int32(instruction.From.Offset)
			}
		}
	}
	if len(c.pcsp) != 0 {
		c.pcsp = append(c.pcsp, jitloader.Pcvalue{PC: uint32(len(code)), Val: stackDelta})
	}
	stats.CodeBytes = len(code)
	c.stats = stats
	return code, nil
}

var (
	_RAX = obj.Addr{Type: obj.TYPE_REG, Reg: x86.REG_AX}
	_RBX = obj.Addr{Type: obj.TYPE_REG, Reg: x86.REG_BX}
	_RCX = obj.Addr{Type: obj.TYPE_REG, Reg: x86.REG_CX}
	_R9  = obj.Addr{Type: obj.TYPE_REG, Reg: x86.REG_R9}
	_R8  = obj.Addr{Type: obj.TYPE_REG, Reg: x86.REG_R8}
	_R10 = obj.Addr{Type: obj.TYPE_REG, Reg: x86.REG_R10}
	_R11 = obj.Addr{Type: obj.TYPE_REG, Reg: x86.REG_R11}
	_R12 = obj.Addr{Type: obj.TYPE_REG, Reg: x86.REG_R12}
	_RDI = obj.Addr{Type: obj.TYPE_REG, Reg: x86.REG_DI}
	_RSI = obj.Addr{Type: obj.TYPE_REG, Reg: x86.REG_SI}
	_RSP = obj.Addr{Type: obj.TYPE_REG, Reg: x86.REG_SP}
	_RBP = obj.Addr{Type: obj.TYPE_REG, Reg: x86.REG_BP}
	_X0  = obj.Addr{Type: obj.TYPE_REG, Reg: x86.REG_X0}
	_Y0  = obj.Addr{Type: obj.TYPE_REG, Reg: x86.REG_Y0}
)

func (c *jitCodeBuilder) machineInstructionCount() int { return len(c.instructions) }

func jitCopyScratch() int16 { return 0 }
