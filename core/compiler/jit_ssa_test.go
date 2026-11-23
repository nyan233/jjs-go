package compiler

import (
	"math/rand"
	"reflect"
	"testing"

	"github.com/twitchyliquid64/golang-asm/obj"
)

func TestJITSSAConstantFolding(t *testing.T) {
	c := newJITCodeBuilder("constants")
	c.movConst(1<<63-1, _RBX)
	c.movConst(17, _RCX) // Interleaving defeats an adjacent peephole pass.
	c.addConst(1, _RBX)
	c.xor(_RCX, _RCX)
	c.mov(_RBX, _RAX)
	c.ret()
	values, live, _ := optimizeJITSSA(c.ssa.values)
	for id, v := range values {
		if live[id] && (v.op == ssaAdd || v.op == ssaXor) {
			t.Fatalf("unfolded operation: %+v", v)
		}
	}
	before, after := interpretJITSSA(c.ssa.values, nil), interpretJITSSA(values, live)
	if !reflect.DeepEqual(before, after) || after.result != uint64(1)<<63 {
		t.Fatalf("overflow folding: before=%+v after=%+v", before, after)
	}
}

func TestJITSSAMemoryVersions(t *testing.T) {
	for _, barrier := range []string{"none", "store", "call", "copy"} {
		t.Run(barrier, func(t *testing.T) {
			c := newJITCodeBuilder("loads")
			c.loadMem(_RSP, 0, _RBX)
			switch barrier {
			case "store":
				c.mov(_RAX, mem(_RSP, 0))
			case "call":
				c.call(_R11)
			case "copy":
				c.vectorMove16(mem(_RSP, 16), mem(_RSP, 0))
			}
			c.loadMem(_RSP, 0, _RAX)
			c.mov(_RBX, mem(_RSP, 32))
			c.ret()
			values, live, _ := optimizeJITSSA(c.ssa.values)
			loads := 0
			for id, v := range values {
				if live[id] && v.op == ssaLoad {
					loads++
				}
			}
			want := 2
			if barrier == "none" {
				want = 1
			}
			if loads != want {
				t.Fatalf("loads=%d, want %d", loads, want)
			}
			if a, b := interpretJITSSA(c.ssa.values, nil), interpretJITSSA(values, live); !reflect.DeepEqual(a, b) {
				t.Fatalf("memory barrier changed behavior: %v != %v", a, b)
			}
		})
	}
}

func TestJITSSAClobberedCopyAndABIArguments(t *testing.T) {
	c := newJITCodeBuilder("clobber")
	c.mov(_RAX, _RBX)
	c.movConst(42, _RAX)
	c.mov(_RBX, mem(_RSP, 0)) // Must still contain the original AX.
	c.mov(_RAX, _RCX)         // A live implicit ABI operand, even with an equivalent AX.
	c.call(_R11)
	c.ret()
	values, live, _ := optimizeJITSSA(c.ssa.values)
	if a, b := interpretJITSSA(c.ssa.values, nil), interpretJITSSA(values, live); !reflect.DeepEqual(a, b) {
		t.Fatalf("register clobber changed behavior: %v != %v", a, b)
	}
}

func TestJITSSAEliminatesUnusedContextReloads(t *testing.T) {
	c := newJITCodeBuilder("dead_context")
	c.functionPrologue(jitFrameSize, 0)
	c.restoreJITRegs()
	c.finishFunction(jitFrameSize)
	values, live, _ := optimizeJITSSA(c.ssa.values)
	frames := 0
	for id, v := range values {
		if !live[id] {
			continue
		}
		if v.op == ssaLoad {
			t.Fatal("unused context load survived")
		}
		if v.op == ssaFrameEnter || v.op == ssaFrameExit {
			frames++
		}
	}
	if frames != 2 {
		t.Fatal("stack frame operation was removed")
	}
}

// This interpreter deliberately reads physical registers at execution time,
// instead of caching values by SSA ID. It catches invalid propagation through
// overwritten homes as well as memory/call ordering errors during lowering.
type jitSSATestResult struct {
	memory map[uint64]byte
	calls  [][7]uint64
	result uint64
}

func interpretJITSSA(values []jitSSAValue, live []bool) jitSSATestResult {
	r := jitSSATestResult{memory: make(map[uint64]byte)}
	regs := make(map[int16]uint64)
	for _, v := range values {
		if v.reg != 0 {
			regs[v.reg] = uint64(v.reg)*100 + 1
		}
	}
	load := func(address uint64, width int) uint64 {
		var result uint64
		for i := 0; i < width/8; i++ {
			result |= uint64(r.memory[address+uint64(i)]) << (8 * i)
		}
		return result
	}
	store := func(address, value uint64, width int) {
		for i := 0; i < width/8; i++ {
			r.memory[address+uint64(i)] = byte(value >> (8 * i))
		}
	}
	for id, v := range values {
		if live != nil && !live[id] {
			continue
		}
		var args [7]uint64
		for i, arg := range v.args[:v.nargs] {
			args[i] = regs[values[arg].reg]
		}
		switch v.op {
		case ssaInput:
		case ssaConst:
			regs[v.reg] = uint64(v.imm)
		case ssaCopy:
			regs[v.reg] = args[0]
		case ssaAdd:
			regs[v.reg] = args[0] + uint64(v.imm)
		case ssaXor:
			regs[v.reg] = args[0] ^ args[1]
		case ssaLoad:
			regs[v.reg] = load(args[0]+uint64(v.imm), 64)
		case ssaStore:
			store(args[1]+uint64(v.imm), args[0], v.width)
		case ssaCopyMemory:
			for i := 0; i < v.width; i += 8 {
				word := load(args[0]+uint64(v.imm)+uint64(i), 64)
				store(args[1]+uint64(v.offset)+uint64(i), word, 64)
				if scratch := jitCopyScratch(); scratch != 0 {
					regs[scratch] = word
				}
			}
		case ssaCall:
			r.calls = append(r.calls, args)
			store(regs[_RSP.Reg], uint64(len(r.calls)), 64)
			for reg := range regs {
				if reg != _RSP.Reg {
					regs[reg] = uint64(reg)*7 + uint64(len(r.calls))
				}
			}
		case ssaReturn:
			r.result = regs[_RAX.Reg]
		default:
			panic("unexpected test operation")
		}
	}
	return r
}

func TestJITSSARandomizedEquivalence(t *testing.T) {
	registers := []obj.Addr{_RAX, _RBX, _RCX, _RDI, _RSI, _R8, _R10, _R11, _R12}
	for seed := int64(0); seed < 300; seed++ {
		rng := rand.New(rand.NewSource(seed))
		c := newJITCodeBuilder("random")
		for i := 0; i < 150; i++ {
			from, to := registers[rng.Intn(len(registers))], registers[rng.Intn(len(registers))]
			offset := int64(rng.Intn(8) * 8)
			switch rng.Intn(10) {
			case 0:
				c.movConst(int64(rng.Uint64()), to)
			case 1:
				c.mov(from, to)
			case 2:
				c.addConst(int64(rng.Intn(17)-8), to)
			case 3:
				c.xor(from, to)
			case 4:
				c.loadMem(_RSP, offset, to)
			case 5:
				c.mov(from, mem(_RSP, offset))
			case 6:
				c.store8(from, mem(_RSP, offset))
			case 7:
				c.call(_R11)
			case 8:
				c.vectorMove16(mem(_RSP, 0), mem(_RSP, 32))
			case 9:
				c.lea(from, offset, to)
			}
		}
		c.ret()
		values, live, _ := optimizeJITSSA(c.ssa.values)
		if a, b := interpretJITSSA(c.ssa.values, nil), interpretJITSSA(values, live); !reflect.DeepEqual(a, b) {
			t.Fatalf("seed %d changed behavior: %v != %v", seed, a, b)
		}
	}
}
