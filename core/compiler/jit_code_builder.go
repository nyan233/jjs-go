package compiler

import (
	"github.com/nyan233/jjs-go/internal/jitloader"
	"github.com/twitchyliquid64/golang-asm/obj"
)

type jitCodegenStats struct {
	SSAValuesBefore     int
	SSAValuesAfter      int
	InstructionsBefore  int
	InstructionsAfter   int
	RemovedInstructions int
	FoldedInstructions  int
	BranchesRemoved     int
	VirtualRegisters    int
	RegisterReuse       int
	SpilledRegisters    int
	CodeBytes           int
}

type jitCodeBuilder struct {
	ssa jitSSA

	name         string
	instructions []*obj.Prog
	virtualCount int
	stats        jitCodegenStats
	codeWords    []uint32
	dataOffset   int64
	pcsp         jitloader.Pcdata
}

func newJITCodeBuilder(name string) *jitCodeBuilder {
	return &jitCodeBuilder{name: name}
}

func (c *jitCodeBuilder) emit(prog *obj.Prog) {
	c.instructions = append(c.instructions, prog)
}

func mem(base obj.Addr, offset int64) obj.Addr {
	return obj.Addr{Type: obj.TYPE_MEM, Reg: base.Reg, Offset: base.Offset + offset}
}

func registerAddr(register int) obj.Addr {
	return obj.Addr{Type: obj.TYPE_REG, Reg: int16(register)}
}
