//go:build arm64 || arm64_asm_test

package compiler

import (
	"encoding/binary"
	"math/rand"
	"reflect"
	"runtime"
	"slices"
	"strconv"
	"testing"

	"github.com/twitchyliquid64/golang-asm/obj/arm64"
)

// A separate scalar ARM64 interpreter checks observable behavior, including
// byte-addressed overlapping stores, flags and control flow. It deliberately
// does not use the optimizer's decoder, value numbering or condition helpers.
type ssaTestMachine struct {
	regs       [32]uint64
	memory     map[uint64]byte
	n, z, c, v bool
}

func (m *ssaTestMachine) reg(r uint32) uint64 {
	if r == 31 {
		return 0
	}
	return m.regs[r]
}
func (m *ssaTestMachine) run(t *testing.T, words []uint32) {
	t.Helper()
	for pc, steps := 0, 0; pc < len(words) && steps < 10000; steps++ {
		w := words[pc]
		d, a, b := w&31, w>>5&31, w>>16&31
		next := pc + 1
		switch {
		case w == 0xd65f03c0:
			return
		case w&0xfc000000 == 0x14000000:
			next = pc + int(int32(w&0x3ffffff)<<6>>6)
		case w&0xff000010 == 0x54000000:
			take := false
			switch w & 15 {
			case 0:
				take = m.z
			case 1:
				take = !m.z
			case 2:
				take = m.c
			case 3:
				take = !m.c
			case 4:
				take = m.n
			case 5:
				take = !m.n
			default:
				t.Fatalf("condition %x", w)
			}
			if take {
				next = pc + int(int32((w>>5)&0x7ffff)<<13>>13)
			}
		case w&0xff800000 == 0xd2800000:
			m.regs[d] = uint64(w>>5&65535) << (16 * (w >> 21 & 3))
		case w&0xff800000 == 0xf2800000:
			shift := 16 * (w >> 21 & 3)
			m.regs[d] = m.regs[d]&^(uint64(65535)<<shift) | uint64(w>>5&65535)<<shift
		case w&0xffe0ffe0 == 0xaa0003e0:
			m.regs[d] = m.reg(b)
		case w&0xff800000 == 0x91000000:
			m.regs[d] = m.regs[a] + uint64(w>>10&4095)<<(12*(w>>22&1))
		case w&0xff800000 == 0xd1000000:
			m.regs[d] = m.regs[a] - uint64(w>>10&4095)<<(12*(w>>22&1))
		case w&0xffe0fc00 == 0x8b000000:
			m.regs[d] = m.reg(a) + m.reg(b)
		case w&0xffe0fc00 == 0xcb000000:
			m.regs[d] = m.reg(a) - m.reg(b)
		case w&0xffe0fc00 == 0xca000000:
			m.regs[d] = m.reg(a) ^ m.reg(b)
		case w&0xffe0fc00 == 0x8a000000:
			m.regs[d] = m.reg(a) & m.reg(b)
		case w&0xffc0fc00 == 0xd340fc00:
			m.regs[d] = m.reg(a) >> (w >> 16 & 63)
		case w&0xffe0fc1f == 0xeb00001f || w&0xff80001f == 0xf100001f:
			left, right := m.reg(a), m.reg(b)
			if w&0xff80001f == 0xf100001f {
				left = m.regs[a]
				right = uint64(w>>10&4095) << (12 * (w >> 22 & 1))
			}
			result := left - right
			m.n = result>>63 != 0
			m.z = result == 0
			m.c = left >= right
			m.v = (left>>63 != right>>63) && (result>>63 != left>>63)
		case w&0x3b000000 == 0x39000000 || w&0x3b200c00 == 0x38000000:
			size := uint(1) << (w >> 30)
			var offset int64
			if w&0x01000000 != 0 {
				offset = int64(w>>10&4095) * int64(size)
			} else {
				offset = int64(int32(w>>12&511) << 23 >> 23)
			}
			addr := m.regs[a] + uint64(offset)
			if w>>22&1 != 0 {
				var value uint64
				for i := uint(0); i < size; i++ {
					value |= uint64(m.memory[addr+uint64(i)]) << (8 * i)
				}
				m.regs[d] = value
			} else {
				for i := uint(0); i < size; i++ {
					m.memory[addr+uint64(i)] = byte(m.reg(d) >> (8 * i))
				}
			}
		default:
			t.Fatalf("unsupported emulator word %08x at %d", w, pc)
		}
		pc = next
	}
	t.Fatal("SSA program did not return")
}

func ssaTestCompile(t *testing.T, c *jitCodeBuilder, enabled bool) ([]uint32, jitCodegenStats) {
	t.Helper()
	copy := *c
	copy.encoderFrame = enabled
	copy.codeWords = slices.Clone(c.codeWords)
	copy.pcsp = nil
	text, err := copy.assemble()
	if err != nil {
		t.Fatal(err)
	}
	words := make([]uint32, len(text)/4)
	for i := range words {
		words[i] = binary.LittleEndian.Uint32(text[i*4:])
	}
	return words, copy.stats
}

func ssaTestCompare(t *testing.T, c *jitCodeBuilder, seed int64) jitCodegenStats {
	t.Helper()
	before, _ := ssaTestCompile(t, c, false)
	after, stats := ssaTestCompile(t, c, true)
	random := rand.New(rand.NewSource(seed))
	a, b := ssaTestMachine{memory: make(map[uint64]byte)}, ssaTestMachine{memory: make(map[uint64]byte)}
	for i := range a.regs {
		a.regs[i] = random.Uint64()
	}
	b.regs = a.regs
	a.run(t, before)
	b.run(t, after)
	if a.regs[0] != b.regs[0] || a.regs[29] != b.regs[29] || a.regs[30] != b.regs[30] || a.regs[31] != b.regs[31] || !reflect.DeepEqual(a.memory, b.memory) {
		t.Fatalf("seed %d: SSA changed observable state: before R0=%x after R0=%x", seed, a.regs[0], b.regs[0])
	}
	return stats
}

func TestARM64SSACopiesAndMemory(t *testing.T) {
	if !jitSSAEnabled {
		t.Skip("SSA disabled")
	}
	c := newJITCodeBuilder("ssa_copies")
	c.movConst(4096, _R12)
	c.movConst(7, _RAX)
	c.mov(_RAX, _RBX)
	c.movConst(99, _RAX) // The original home is overwritten.
	c.mov(_RBX, mem(_R12, 0))
	c.loadMem(_R12, 0, _RCX) // Can forward only from the still-live BX.
	c.movConst(1, _RBX)
	c.store8(_RBX, mem(_R12, 1)) // Aliasing partial store invalidates a full load.
	c.loadMem(_R12, 0, _RAX)
	c.add(_RCX, _RAX)
	c.movConst(88, _RDI) // Dead definition.
	c.ret()
	stats := ssaTestCompare(t, c, 7)
	if stats.RemovedInstructions < 2 || stats.FoldedInstructions == 0 {
		t.Fatalf("no useful SSA optimization: %+v", stats)
	}
}

func TestARM64SSAControlFlow(t *testing.T) {
	if !jitSSAEnabled {
		t.Skip("SSA disabled")
	}
	for seed := int64(0); seed < 500; seed++ {
		rng := rand.New(rand.NewSource(seed))
		c := newJITCodeBuilder("ssa_random")
		base := registerAddr(arm64.REG_R27)
		c.movConst(4096, base)
		c.movConst(0, _RAX)
		c.movConst(5, _R8)
		loop, done, other, join := c.newLabel(), c.newLabel(), c.newLabel(), c.newLabel()
		c.bind(loop)
		c.compareConst(0, _R8)
		c.branch(jitEqual, done)
		for i := 0; i < 40; i++ {
			a, b := registerAddr(arm64.REG_R0+rng.Intn(5)), registerAddr(arm64.REG_R0+rng.Intn(5))
			switch rng.Intn(10) {
			case 0:
				c.movConst(int64(rng.Uint64()), a)
			case 1:
				c.mov(a, b)
			case 2:
				c.addConst(int64(rng.Intn(100)-50), a)
			case 3:
				c.add(a, b)
			case 4:
				c.subtract(a, b)
			case 5:
				c.xor(a, b)
			case 6:
				c.shiftRight(rng.Intn(64), a)
			case 7:
				c.mov(a, mem(base, int64(rng.Intn(8)*8)))
			case 8:
				c.loadMem(base, int64(rng.Intn(8)*8), a)
			case 9:
				c.store8(a, mem(base, int64(rng.Intn(64))))
			}
		}
		c.compare(_RAX, _RBX)
		c.branch(jitBelow, other)
		c.addConst(3, _RAX)
		c.branch(jitAlways, join)
		c.bind(other)
		c.addConst(11, _RAX)
		c.bind(join)
		c.subConst(1, _R8)
		c.branch(jitAlways, loop)
		c.bind(done)
		c.ret()
		stats := ssaTestCompare(t, c, seed)
		if stats.SSAPhis == 0 || stats.SSABlocks < 5 {
			t.Fatalf("missing CFG/phi nodes: %+v", stats)
		}
	}
}

func TestARM64SSAUnknownBranch(t *testing.T) {
	c := newJITCodeBuilder("ssa_unknown")
	c.word(0xb4000040) // CBZ belongs to an opaque kernel, not the scalar decoder.
	c.movConst(1, _RAX)
	c.ret()
	before, _ := ssaTestCompile(t, c, false)
	after, stats := ssaTestCompile(t, c, true)
	if !slices.Equal(before, after) || stats.SSAValues != 0 {
		t.Fatal("unknown control flow was modified")
	}
}

func TestARM64SSAUnknownZeroWord(t *testing.T) {
	c := newJITCodeBuilder("ssa_unknown_zero")
	c.word(0) // Undefined/trapping instruction, not an optimizer-created nop.
	c.ret()
	before, _ := ssaTestCompile(t, c, false)
	after, _ := ssaTestCompile(t, c, true)
	if !slices.Equal(before, after) {
		t.Fatal("SSA removed an opaque instruction")
	}
}

func TestARM64SSAHelperArguments(t *testing.T) {
	functions := [jitHelperCount]any{
		jitHelperString: jitAppendString, jitHelperInt: jitAppendIntPtr,
		jitHelperFloat: jitAppendFloatPtr, jitHelperBool: jitAppendBoolPtr,
		jitHelperNull: jitAppendNull, jitHelperPointer: jitEncodePointer,
		jitHelperSlice: jitEncodeSlice, jitHelperInterface: jitEncodeInterface,
		jitHelperReserve: jitReserveRaw, jitHelperByteSlice: jitEncodeByteSlice,
		jitHelperResolvedField: jitEncodeResolvedField, jitHelperCustomMarshal: jitEncodeCustomMarshaler,
		jitHelperFieldPrefix: jitWriteFieldPrefix, jitHelperFloatFormat: strconv.AppendFloat,
		jitHelperYield: runtime.Gosched, jitHelperMapStart: jitMapStart,
		jitHelperMapNext: (*reflect.MapIter).Next, jitHelperMapEnd: jitMapEnd,
		jitHelperValue: jitEncodeValue,
	}
	for index, fn := range functions {
		if fn == nil {
			continue
		} // Native quote signatures are checked separately.
		typ := reflect.TypeOf(fn)
		ints, floats := 0, 0
		for i := 0; i < typ.NumIn(); i++ {
			switch typ.In(i).Kind() {
			case reflect.Slice:
				ints += 3
			case reflect.Float64:
				floats++
			case reflect.Pointer, reflect.UnsafePointer, reflect.Int, reflect.Uint8:
				ints++
			default:
				t.Fatalf("unmodeled helper argument: %v", typ.In(i))
			}
		}
		if ints != armEncoderArgs[index] || (floats > 0) != (index == jitHelperFloatFormat) {
			t.Fatalf("helper %d ABI: %d integer, %d float arguments", index, ints, floats)
		}
	}
}

func TestARM64SSAConstantBranches(t *testing.T) {
	if !jitSSAEnabled {
		t.Skip("SSA disabled")
	}
	for _, condition := range []int{jitEqual, jitNotEqual, jitBelow, jitAboveEqual, jitNegative, jitNonNegative} {
		for _, a := range []int64{0, 1, -1, -9223372036854775808, 9223372036854775807} {
			c := newJITCodeBuilder("ssa_constant_branch")
			other, done := c.newLabel(), c.newLabel()
			c.movConst(a, _RAX)
			c.movConst(-1, _RBX)
			c.compare(_RAX, _RBX)
			c.branch(condition, other)
			c.movConst(42, _RAX)
			c.branch(jitAlways, done)
			c.bind(other)
			c.movConst(99, _RAX)
			c.bind(done)
			c.ret()
			stats := ssaTestCompare(t, c, a)
			if stats.BranchesRemoved == 0 {
				t.Fatal("constant branch was not folded")
			}
		}
	}
}

func TestARM64SSAOpaqueEntry(t *testing.T) {
	if !jitSSAEnabled {
		t.Skip("SSA disabled")
	}
	c := newJITCodeBuilder("ssa_opaque_entry")
	c.word(0x14000002)
	c.movConst(1, _RAX)
	c.movConst(2, _RAX)
	c.ret()
	c.opaqueRanges = []jitCodeRange{{1, 3}}
	before, _ := ssaTestCompile(t, c, false)
	after, stats := ssaTestCompile(t, c, true)
	if !slices.Equal(before, after) || stats.SSAValues != 0 {
		t.Fatal("branch into an opaque kernel was optimized")
	}
}

func TestARM64SSAEntryBackedge(t *testing.T) {
	if !jitSSAEnabled {
		t.Skip("SSA disabled")
	}
	c := newJITCodeBuilder("ssa_entry_backedge")
	entry, done := c.newLabel(), c.newLabel()
	c.bind(entry)
	c.compareConst(0, _RAX)
	c.branch(jitEqual, done)
	c.movConst(0, _RAX)
	c.addConst(1, _RBX)
	c.branch(jitAlways, entry)
	c.bind(done)
	c.mov(_RBX, _RAX)
	c.ret()
	for _, enabled := range []bool{false, true} {
		words, _ := ssaTestCompile(t, c, enabled)
		machine := ssaTestMachine{memory: make(map[uint64]byte)}
		machine.regs[0] = 3
		machine.run(t, words)
		if machine.regs[0] != 1 {
			t.Fatalf("SSA=%v: lost function-entry phi input", enabled)
		}
	}
}
