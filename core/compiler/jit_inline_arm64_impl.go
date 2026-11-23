//go:build arm64 || arm64_asm_test

package compiler

import (
	"github.com/twitchyliquid64/golang-asm/obj"
	"unsafe"
)

func (c *jitCodeBuilder) bind(l *jitLabel) { l.position = len(c.codeWords) }

func (c *jitCodeBuilder) branch(condition int, l *jitLabel) {
	l.branches = append(l.branches, len(c.codeWords))
	if condition == jitAlways {
		c.word(0x14000000)
		return
	}
	cond := [...]uint32{0, 1, 3, 2, 4, 5}[condition]
	c.word(0x54000000 | cond)
}

func (c *jitCodeBuilder) compare(left, right obj.Addr) {
	c.word(0xeb00001f | arm64Register(right)<<16 | arm64Register(left)<<5)
}

func (c *jitCodeBuilder) compareConst(value int64, reg obj.Addr) {
	if value >= 0 && value <= 4095 {
		c.word(0xf100001f | uint32(value)<<10 | arm64Register(reg)<<5)
	} else {
		c.movConst(value, _RT2)
		c.compare(reg, _RT2)
	}
}

func (c *jitCodeBuilder) add(from, to obj.Addr) {
	c.word(0x8b000000 | arm64Register(from)<<16 | arm64Register(to)<<5 | arm64Register(to))
}

func (c *jitCodeBuilder) subtract(from, to obj.Addr) {
	c.word(0xcb000000 | arm64Register(from)<<16 | arm64Register(to)<<5 | arm64Register(to))
}

func (c *jitCodeBuilder) negate(reg obj.Addr) {
	c.word(0xcb0003e0 | arm64Register(reg)<<16 | arm64Register(reg))
}

func (c *jitCodeBuilder) shiftRight(n int, reg obj.Addr) {
	r := arm64Register(reg)
	c.word(0xd340fc00 | uint32(n)<<16 | r<<5 | r)
}

func (c *jitCodeBuilder) mask(value int64, reg obj.Addr) {
	c.movConst(value, _RT2)
	r := arm64Register(reg)
	c.word(0x8a000000 | arm64Register(_RT2)<<16 | r<<5 | r)
}

func (c *jitCodeBuilder) loadInteger(base obj.Addr, offset int64, width int, signed bool, to obj.Addr) {
	if width == 8 {
		c.loadMem(base, offset, to)
		return
	}
	c.lea(base, offset, _RT2)
	op := uint32(0x39400000)
	if width == 2 {
		op = 0x79400000
	}
	if width == 4 {
		op = 0xb9400000
	}
	if signed {
		op += 0x400000
	}
	c.word(op | arm64Register(_RT2)<<5 | arm64Register(to))
}

// Quotient of BX / 100 in CX, remainder in SI. The reciprocal is exact for
// every uint64, including the magnitude of MinInt64.
func (c *jitCodeBuilder) divide100() {
	c.movConst(int64(-6640827866535438581), _RSI) // 0xa3d70a3d70a3d70b
	c.mov(_RBX, _RAX)
	c.shiftRight(2, _RAX)
	c.word(0x9bc07c00 | arm64Register(_RSI)<<16 | arm64Register(_RAX)<<5 | arm64Register(_RCX))
	c.shiftRight(4, _RCX)
	c.movConst(100, _RSI)
	// MSUB SI, CX, SI, BX
	c.word(0x9b008000 | arm64Register(_RSI)<<16 | arm64Register(_RBX)<<10 | arm64Register(_RCX)<<5 | arm64Register(_RSI))
}

func (c *jitCodeBuilder) loadFloat(base obj.Addr, offset int64, width int) {
	c.lea(base, offset, _RT2)
	if width == 4 {
		c.word(0xbd400000 | arm64Register(_RT2)<<5) // LDR S0
		c.word(0x1e22c000)                          // FCVT D0, S0
	} else {
		c.word(0xfd400000 | arm64Register(_RT2)<<5)
	}
	c.word(0x9e660000 | arm64Register(_RBX)) // FMOV BX, D0
}

func (c *jitCodeBuilder) floatBitsArgument(bits int64) { c.movConst(bits, _R11) }

func (g *jitEncoderGenerator) emitString(offset int64) {
	c := g.code
	c.loadMem(_R12, offset+8, _RBX)
	c.addConst(2, _RBX)
	g.emitReserveVariable()
	c.movConst('"', _RBX)
	c.store8(_RBX, mem(_RAX, 0))
	c.addConst(1, _RAX)
	// Publish only the initialized opening quote before entering Go/assembly.
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
	// Return values: AX=consumed, BX=written. Reload roots after the call.
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
	c := g.code
	nilSlice, slow, done := c.newLabel(), c.newLabel(), c.newLabel()
	c.loadMem(_R12, offset, _RBX)
	c.compareConst(0, _RBX)
	c.branch(jitEqual, nilSlice)
	c.loadMem(_R12, offset+8, _RBX)
	c.compareConst(base64ChunkBytes+1, _RBX)
	c.branch(jitAboveEqual, slow)
	// The bounded length makes this arithmetic overflow-free.
	c.addConst(2, _RBX)
	c.movConst(3, _RDI)
	c.word(0x9ac00800 | arm64Register(_RDI)<<16 | arm64Register(_RBX)<<5 | arm64Register(_RBX)) // UDIV
	c.add(_RBX, _RBX)
	c.add(_RBX, _RBX)
	c.addConst(2, _RBX)
	g.emitReserveVariable()
	c.movConst('"', _RBX)
	c.store8(_RBX, mem(_RAX, 0))
	c.addConst(1, _RAX)
	c.loadMem(_R12, offset, _RBX)
	c.loadMem(_R12, offset+8, _RCX)
	c.loadMem(_R10, int64(unsafe.Offsetof(jitEncodeContext{}.program)), _RDI)
	c.loadMem(_RDI, int64(unsafe.Offsetof(jitEncodeProgram{}.base64)), _RDI)
	for _, word := range base64Kernel {
		c.word(word)
	}
	c.movConst('"', _RBX)
	c.store8(_RBX, mem(_RAX, 0))
	c.branch(jitAlways, done)
	c.bind(nilSlice)
	g.emitRaw([]byte("null"))
	g.flushRaw()
	c.branch(jitAlways, done)
	c.bind(slow)
	c.mov(_R10, _RAX)
	c.lea(_R12, offset, _RBX)
	g.emitHelper(jitHelperByteSlice)
	c.bind(done)
}
