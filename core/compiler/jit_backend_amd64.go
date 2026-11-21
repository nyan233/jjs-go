//go:build amd64 && !arm64_asm_test

package compiler

import (
	golangasm "github.com/twitchyliquid64/golang-asm"
	"github.com/twitchyliquid64/golang-asm/obj"
	"github.com/twitchyliquid64/golang-asm/obj/x86"
)

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

func (c *jitCodeBuilder) store32(from, to obj.Addr) {
	c.emit(&obj.Prog{As: x86.AMOVL, From: from, To: to})
}

func (c *jitCodeBuilder) store16(from, to obj.Addr) {
	c.emit(&obj.Prog{As: x86.AMOVW, From: from, To: to})
}

func (c *jitCodeBuilder) store8(from, to obj.Addr) {
	c.emit(&obj.Prog{As: x86.AMOVB, From: from, To: to})
}

func (c *jitCodeBuilder) functionPrologue(frameSize, dataOffset int64) {
	c.dataOffset = dataOffset
	c.subConst(frameSize, _RSP)
	c.mov(_RAX, mem(_RSP, 0))
	c.mov(_RAX, _R10)
	c.loadMem(_R10, dataOffset, _R12)
}

func (c *jitCodeBuilder) finishFunction(frameSize int64) {
	c.addConst(frameSize, _RSP)
	c.xor(_RAX, _RAX)
	c.ret()
}

func jitLocalPointers() []bool {
	return []bool{true, false, false, false, false, false, false, false}
}

func (c *jitCodeBuilder) restoreJITRegs() {
	c.loadMem(_RSP, 0, _R10)
	c.loadMem(_R10, c.dataOffset, _R12)
}

func (c *jitCodeBuilder) loadJITContext(to obj.Addr) {
	c.loadMem(_RSP, 0, to)
}

func (c *jitCodeBuilder) call(reg obj.Addr) {
	c.emit(&obj.Prog{As: obj.ACALL, To: reg})
}

func (c *jitCodeBuilder) ret() {
	c.emit(&obj.Prog{As: obj.ARET})
}

func (c *jitCodeBuilder) assemble() ([]byte, error) {
	instructions, stats, err := c.prepareIR()
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
	_X0  = obj.Addr{Type: obj.TYPE_REG, Reg: x86.REG_X0}
	_Y0  = obj.Addr{Type: obj.TYPE_REG, Reg: x86.REG_Y0}
)
