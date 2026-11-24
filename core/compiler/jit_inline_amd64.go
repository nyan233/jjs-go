//go:build amd64 && !arm64_asm_test

package compiler

import (
	"github.com/twitchyliquid64/golang-asm/obj"
	"github.com/twitchyliquid64/golang-asm/obj/x86"
)

func (c *jitCodeBuilder) bind(l *jitLabel) { c.emit(l.prog) }

func (c *jitCodeBuilder) branch(condition int, l *jitLabel) {
	op := [...]obj.As{x86.AJEQ, x86.AJNE, x86.AJCS, x86.AJCC, x86.AJMI, x86.AJPL, obj.AJMP}[condition]
	c.emit(&obj.Prog{As: op, To: obj.Addr{Type: obj.TYPE_BRANCH, Val: l.prog}})
}

func (c *jitCodeBuilder) compare(left, right obj.Addr) {
	c.emit(&obj.Prog{As: x86.ACMPQ, From: left, To: right})
}
func (c *jitCodeBuilder) compareConst(value int64, reg obj.Addr) {
	if value >= -2147483648 && value <= 2147483647 {
		c.emit(&obj.Prog{As: x86.ACMPQ, From: reg, To: obj.Addr{Type: obj.TYPE_CONST, Offset: value}})
	} else {
		c.movConst(value, _RT2)
		c.compare(reg, _RT2)
	}
}
func (c *jitCodeBuilder) add(from, to obj.Addr) { c.emit(&obj.Prog{As: x86.AADDQ, From: from, To: to}) }
func (c *jitCodeBuilder) subtract(from, to obj.Addr) {
	c.emit(&obj.Prog{As: x86.ASUBQ, From: from, To: to})
}
func (c *jitCodeBuilder) negate(reg obj.Addr) { c.emit(&obj.Prog{As: x86.ANEGQ, To: reg}) }
func (c *jitCodeBuilder) shiftRight(n int, reg obj.Addr) {
	c.emit(&obj.Prog{As: x86.ASHRQ, From: obj.Addr{Type: obj.TYPE_CONST, Offset: int64(n)}, To: reg})
}
func (c *jitCodeBuilder) mask(value int64, reg obj.Addr) {
	c.movConst(value, _RT2)
	c.emit(&obj.Prog{As: x86.AANDQ, From: _RT2, To: reg})
}
func (c *jitCodeBuilder) loadInteger(base obj.Addr, offset int64, width int, signed bool, to obj.Addr) {
	op := x86.AMOVQ
	switch width {
	case 1:
		op = x86.AMOVBQZX
		if signed {
			op = x86.AMOVBQSX
		}
	case 2:
		op = x86.AMOVWQZX
		if signed {
			op = x86.AMOVWQSX
		}
	case 4:
		op = x86.AMOVL
		if signed {
			op = x86.AMOVLQSX
		}
	}
	c.emit(&obj.Prog{As: op, From: mem(base, offset), To: to})
}
func (c *jitCodeBuilder) divide100() {
	c.mov(_RBX, _RAX)
	c.shiftRight(2, _RAX)
	c.movConst(-6640827866535438581, _RSI)
	c.emit(&obj.Prog{As: x86.AMULQ, From: _RSI})
	c.mov(registerAddr(x86.REG_DX), _RCX)
	c.shiftRight(4, _RCX)
	c.movConst(100, _RSI)
	c.emit(&obj.Prog{As: x86.AIMULQ, From: _RCX, To: _RSI})
	c.negate(_RSI)
	c.add(_RBX, _RSI)
}
func (c *jitCodeBuilder) loadFloat(base obj.Addr, offset int64, width int) {
	op := x86.AMOVSD
	if width == 4 {
		op = x86.ACVTSS2SD
	}
	c.emit(&obj.Prog{As: op, From: mem(base, offset), To: _X0})
	c.emit(&obj.Prog{As: x86.AMOVQ, From: _X0, To: _RBX})
}
func (c *jitCodeBuilder) floatBitsArgument(bits int64) { c.movConst(bits, _R8) }

func (g *jitEncoderGenerator) emitString(offset int64) {
	c := g.code
	c.loadMem(_R12, offset+8, _RBX)
	c.addConst(2, _RBX)
	g.emitReserveVariable()
	c.movConst('"', _RBX)
	c.store8(_RBX, mem(_RAX, 0))
	c.addConst(1, _RAX)
	// Publish only the initialized opening quote before entering assembly.
	c.loadMem(_R10, 0, _RCX)
	c.mov(_RAX, _RDI)
	c.subtract(_RCX, _RDI)
	c.mov(_RDI, mem(_R10, 8))
	c.loadMem(_R10, 16, _RBX)
	c.subtract(_RDI, _RBX)
	c.subConst(1, _RBX) // Preserve one byte for the closing quote.
	c.loadMem(_R12, offset, _RCX)
	c.loadMem(_R12, offset+8, _RDI)
	g.emitHelper(jitHelperQuote)
	// Return values: AX=consumed, BX=written. Reload roots afterward.
	c.loadMem(_R10, 8, _RCX)
	c.add(_RBX, _RCX)
	c.mov(_RCX, mem(_R10, 8))
	c.loadMem(_R12, offset+8, _RDI)
	c.compare(_RAX, _RDI)
	complete, done := c.newLabel(), c.newLabel()
	c.branch(jitEqual, complete)
	c.loadMem(_R12, offset, _RBX)
	c.add(_RAX, _RBX)
	c.subtract(_RAX, _RDI)
	c.mov(_RDI, _RCX)
	c.mov(_R10, _RAX)
	g.emitHelper(jitHelperQuoteTail)
	c.branch(jitAlways, done)
	c.bind(complete)
	c.loadMem(_R10, 0, _RAX)
	c.add(_RCX, _RAX)
	c.movConst('"', _RBX)
	c.store8(_RBX, mem(_RAX, 0))
	c.addConst(1, _RCX)
	c.mov(_RCX, mem(_R10, 8))
	c.bind(done)
}

func (g *jitEncoderGenerator) emitByteSlice(offset int64) {
	g.code.mov(_R10, _RAX)
	g.code.lea(_R12, offset, _RBX)
	g.emitHelper(jitHelperByteSlice)
}
