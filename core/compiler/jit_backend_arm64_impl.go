//go:build arm64 || arm64_asm_test

package compiler

import (
	"encoding/binary"

	"github.com/nyan233/jjs-go/internal/jitloader"
	"github.com/twitchyliquid64/golang-asm/obj"
	"github.com/twitchyliquid64/golang-asm/obj/arm64"
)

const (
	// Go helpers can spill six register arguments into SP+8 through SP+48.
	// The caller's saved FP occupies SP+72. Keep our context between them.
	jitFrameSize    = 80
	jitFrameLink    = 0
	jitFramePointer = -8
	jitFrameContext = 64

	arm64LinkRegister  = 30
	arm64FrameRegister = 29
	arm64StackPointer  = 31
	arm64ZeroRegister  = 31
)

func (c *jitCodeBuilder) machineMov(from, to obj.Addr) {
	switch {
	case from.Type == obj.TYPE_REG && to.Type == obj.TYPE_REG:
		c.word(0xaa0003e0 | arm64Register(from)<<16 | arm64Register(to))
	case from.Type == obj.TYPE_MEM && to.Type == obj.TYPE_REG:
		c.loadMemory(from, to, false)
	case from.Type == obj.TYPE_REG && to.Type == obj.TYPE_MEM:
		c.storeMemory(from, to, 64)
	default:
		panic("arm64 JIT: unsupported MOVD operands")
	}
}

func (c *jitCodeBuilder) machineVectorMove32(from, to obj.Addr) {
	c.copyWords(from, to, 32)
}

func (c *jitCodeBuilder) machineVectorMove16(from, to obj.Addr) {
	c.copyWords(from, to, 16)
}

func (c *jitCodeBuilder) copyWords(from, to obj.Addr, size int) {
	for offset := 0; offset < size; offset += 8 {
		c.loadMemory(mem(from, int64(offset)), _R7, false)
		c.storeMemory(_R7, mem(to, int64(offset)), 64)
	}
}

func (c *jitCodeBuilder) machineMovConst(value int64, to obj.Addr) {
	raw := uint64(value)
	register := arm64Register(to)
	first := true
	for shift := 0; shift < 64; shift += 16 {
		part := uint16(raw >> shift)
		if part == 0 {
			continue
		}
		word := uint32(0xf2800000) // MOVK
		if first {
			word = 0xd2800000 // MOVZ
			first = false
		}
		word |= uint32(part)<<5 | uint32(shift/16)<<21 | register
		c.word(word)
	}
	if first {
		c.word(0xd2800000 | register)
	}
}

func (c *jitCodeBuilder) machineLoadMem(base obj.Addr, offset int64, to obj.Addr) {
	c.loadMemory(mem(base, offset), to, false)
}

func (c *jitCodeBuilder) machineLea(base obj.Addr, offset int64, to obj.Addr) {
	c.addImmediate(arm64Register(base), offset, arm64Register(to))
}

func (c *jitCodeBuilder) machineXor(from, to obj.Addr) {
	source := arm64Register(from)
	destination := arm64Register(to)
	if source == destination {
		c.word(0xaa0003e0 | arm64ZeroRegister<<16 | destination)
		return
	}
	c.word(0xca000000 | source<<16 | destination<<5 | destination)
}

func (c *jitCodeBuilder) machineStore32(from, to obj.Addr) {
	c.storeMemory(from, to, 32)
}

func (c *jitCodeBuilder) machineStore16(from, to obj.Addr) {
	c.storeMemory(from, to, 16)
}

func (c *jitCodeBuilder) machineStore8(from, to obj.Addr) {
	c.storeMemory(from, to, 8)
}

func (c *jitCodeBuilder) machineFunctionPrologue(frameSize int64) {
	if frameSize != jitFrameSize {
		panic("arm64 JIT: invalid frame size")
	}
	// STR X30, [SP, #-80]! allocates the frame and preserves the link register.
	c.word(0xf81b0ffe)
	c.pcsp = jitloader.Pcdata{{PC: uint32(len(c.codeWords) * 4), Val: 0}}
	c.storeMemory(_R29, mem(_RSP, jitFramePointer), 64)
	c.storeMemory(_RAX, mem(_RSP, jitFrameContext), 64)
	c.addImmediate(arm64StackPointer, jitFramePointer, arm64FrameRegister)
}

func (c *jitCodeBuilder) machineFinishFunction(frameSize int64) {
	c.machineMov(_ZR, _RAX)
	c.loadMemory(mem(_RSP, jitFramePointer), _R29, false)
	c.loadMemory(mem(_RSP, jitFrameLink), _R30, false)
	c.addImmediate(arm64StackPointer, frameSize, arm64StackPointer)
	c.pcsp = append(c.pcsp, jitloader.Pcvalue{PC: uint32(len(c.codeWords) * 4), Val: int32(frameSize)})
	c.machineRet()
}

func jitLocalPointers() []bool {
	// Local maps end at frame.varp = entry SP - 8, so this bit covers SP+64.
	return []bool{true}
}

func (c *jitCodeBuilder) machineCall(reg obj.Addr) {
	c.word(0xd63f0000 | arm64Register(reg)<<5)
}

func (c *jitCodeBuilder) machineRet() {
	c.word(0xd65f0000 | arm64LinkRegister<<5)
}

func (c *jitCodeBuilder) newVirtualRegister() obj.Addr {
	return _R8
}

func (c *jitCodeBuilder) assemble() ([]byte, error) {
	c.lowerSSA()
	code := make([]byte, len(c.codeWords)*4)
	for index, word := range c.codeWords {
		binary.LittleEndian.PutUint32(code[index*4:], word)
	}
	if padding := (16 - len(code)%16) % 16; padding != 0 {
		code = append(code, make([]byte, padding)...)
	}
	if len(c.pcsp) != 0 {
		c.pcsp = append(c.pcsp, jitloader.Pcvalue{PC: uint32(len(code)), Val: 0})
	}
	c.stats.CodeBytes = len(code)
	return code, nil
}

func (c *jitCodeBuilder) word(word uint32) {
	c.codeWords = append(c.codeWords, word)
}

func (c *jitCodeBuilder) loadMemory(from, to obj.Addr, wide bool) {
	offset := from.Offset
	base := arm64Register(from)
	if scaled, ok := scaledOffset(offset); ok && !wide {
		c.word(0xf9400000 | scaled<<10 | base<<5 | arm64Register(to))
		return
	}
	if offset >= -256 && offset <= 255 {
		c.word(0xf8400000 | uint32(offset&0x1ff)<<12 | base<<5 | arm64Register(to))
		return
	}
	c.addImmediate(base, offset, 16)
	c.loadMemory(mem(registerAddr(arm64.REG_R16), 0), to, wide)
}

func (c *jitCodeBuilder) storeMemory(from, to obj.Addr, width int) {
	offset := to.Offset
	base := arm64Register(to)
	if width == 64 {
		if scaled, ok := scaledOffset(offset); ok {
			c.word(0xf9000000 | scaled<<10 | base<<5 | arm64Register(from))
			return
		}
	}
	if offset >= -256 && offset <= 255 {
		baseOp := uint32(0xf8000000)
		switch width {
		case 32:
			baseOp = 0xb9000000
		case 16:
			baseOp = 0x79000000
		case 8:
			baseOp = 0x39000000
		}
		c.word(baseOp | uint32(offset&0x1ff)<<12 | base<<5 | arm64Register(from))
		return
	}
	c.addImmediate(base, offset, 16)
	c.storeMemory(from, mem(registerAddr(arm64.REG_R16), 0), width)
}

func (c *jitCodeBuilder) addImmediate(register uint32, value int64, destination uint32) {
	if value == 0 {
		if register != destination {
			c.word(0xaa0003e0 | register<<16 | destination)
		}
		return
	}
	if value > 0 && value <= 0xfff {
		c.word(0x91000000 | uint32(value)<<10 | register<<5 | destination)
		return
	}
	if value > 0xfff && value&0xfff == 0 && value>>12 <= 0xfff {
		c.word(0x91400000 | uint32(value>>12)<<10 | register<<5 | destination)
		return
	}
	if value < 0 && -value <= 0xfff {
		c.word(0xd1000000 | uint32(-value)<<10 | register<<5 | destination)
		return
	}
	c.machineMovConst(value, registerAddr(arm64.REG_R16))
	c.word(0x8b000000 | 16<<16 | register<<5 | destination)
}

func scaledOffset(offset int64) (uint32, bool) {
	if offset < 0 || offset%8 != 0 {
		return 0, false
	}
	scaled := uint32(offset / 8)
	return scaled, scaled <= 0xfff
}

func arm64Register(address obj.Addr) uint32 {
	if address.Reg == arm64.REGSP || address.Reg == arm64.REGZERO {
		return arm64StackPointer
	}
	return uint32(address.Reg-arm64.REG_R0) & 31
}

var (
	_RAX = registerAddr(arm64.REG_R0)
	_RBX = registerAddr(arm64.REG_R1)
	_RCX = registerAddr(arm64.REG_R2)
	_RDI = registerAddr(arm64.REG_R3)
	_RSI = registerAddr(arm64.REG_R4)
	_R7  = registerAddr(arm64.REG_R7)
	_R11 = registerAddr(arm64.REG_R5)
	_R8  = registerAddr(arm64.REG_R6)
	_R10 = registerAddr(arm64.REG_R8)
	_R12 = registerAddr(arm64.REG_R9)
	_R29 = registerAddr(arm64.REG_R29)
	_R30 = registerAddr(arm64.REG_R30)
	_RSP = registerAddr(arm64.REGSP)
	_ZR  = registerAddr(arm64.REGZERO)
)

func (c *jitCodeBuilder) machineInstructionCount() int { return len(c.codeWords) }

func jitCopyScratch() int16 { return _R7.Reg }
