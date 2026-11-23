package compiler

import (
	"slices"

	"github.com/twitchyliquid64/golang-asm/obj"
)

// The generators currently emit a single basic block: all branches and loops
// live in Go helpers. Each register definition has a distinct SSA value, and
// stores/calls define a new memory version. Physical homes preserve the Go ABI;
// propagation may only use a value while its register still contains it.
// There are no phi nodes, speculative loads, or values live across helper calls.
type jitSSA struct {
	values    []jitSSAValue
	registers map[int16]int
	memory    int // Value ID + 1; zero is the entry memory state.
}

type jitSSAOp uint8

const (
	ssaInput jitSSAOp = iota
	ssaConst
	ssaCopy
	ssaAdd
	ssaXor
	ssaLoad
	ssaStore
	ssaCopyMemory
	ssaCall
	ssaFrameEnter
	ssaFrameExit
	ssaReturn
)

type jitSSAValue struct {
	op     jitSSAOp
	reg    int16
	args   [7]int
	nargs  int
	imm    int64
	offset int64 // Destination offset for CopyMemory.
	width  int
	memory int
}

func (v jitSSAValue) effect() bool { return v.op >= ssaStore }

func (s *jitSSA) read(reg int16) int {
	if id, ok := s.registers[reg]; ok {
		return id
	}
	return s.append(jitSSAValue{op: ssaInput, reg: reg})
}

func (s *jitSSA) append(v jitSSAValue) int {
	if s.registers == nil {
		s.registers = make(map[int16]int)
	}
	id := len(s.values)
	if v.op == ssaLoad || v.effect() {
		v.memory = s.memory
	}
	s.values = append(s.values, v)
	if v.effect() {
		s.memory = id + 1
	} else {
		s.registers[v.reg] = id
	}
	return id
}

func (c *jitCodeBuilder) ssaValue(op jitSSAOp, to obj.Addr, imm int64, from ...obj.Addr) {
	v := jitSSAValue{op: op, reg: to.Reg, imm: imm, nargs: len(from)}
	for i, arg := range from {
		v.args[i] = c.ssa.read(arg.Reg)
	}
	c.ssa.append(v)
}

func (c *jitCodeBuilder) mov(from, to obj.Addr) {
	switch {
	case from.Type == obj.TYPE_REG && to.Type == obj.TYPE_REG:
		c.ssaValue(ssaCopy, to, 0, from)
	case from.Type == obj.TYPE_MEM && to.Type == obj.TYPE_REG:
		c.ssaValue(ssaLoad, to, from.Offset, from)
	case from.Type == obj.TYPE_REG && to.Type == obj.TYPE_MEM:
		c.ssaStore(from, to, 64)
	default:
		panic("JIT SSA: unsupported move operands")
	}
}

func (c *jitCodeBuilder) movConst(value int64, to obj.Addr) { c.ssaValue(ssaConst, to, value) }
func (c *jitCodeBuilder) loadImm64(value uint64)            { c.movConst(int64(value), _R11) }
func (c *jitCodeBuilder) loadMem(base obj.Addr, offset int64, to obj.Addr) {
	c.mov(mem(base, offset), to)
}
func (c *jitCodeBuilder) lea(base obj.Addr, offset int64, to obj.Addr) {
	c.ssaValue(ssaAdd, to, base.Offset+offset, base)
}
func (c *jitCodeBuilder) addConst(value int64, to obj.Addr) { c.ssaValue(ssaAdd, to, value, to) }
func (c *jitCodeBuilder) subConst(value int64, to obj.Addr) { c.addConst(-value, to) }
func (c *jitCodeBuilder) xor(from, to obj.Addr)             { c.ssaValue(ssaXor, to, 0, to, from) }
func (c *jitCodeBuilder) store8(from, to obj.Addr)          { c.ssaStore(from, to, 8) }
func (c *jitCodeBuilder) store16(from, to obj.Addr)         { c.ssaStore(from, to, 16) }
func (c *jitCodeBuilder) store32(from, to obj.Addr)         { c.ssaStore(from, to, 32) }

func (c *jitCodeBuilder) ssaStore(from, to obj.Addr, width int) {
	v := jitSSAValue{op: ssaStore, imm: to.Offset, width: width, nargs: 2}
	v.args[0], v.args[1] = c.ssa.read(from.Reg), c.ssa.read(to.Reg)
	c.ssa.append(v)
}

func (c *jitCodeBuilder) vectorMove16(from, to obj.Addr) { c.ssaCopyMemory(from, to, 16) }
func (c *jitCodeBuilder) vectorMove32(from, to obj.Addr) { c.ssaCopyMemory(from, to, 32) }
func (c *jitCodeBuilder) ssaCopyMemory(from, to obj.Addr, width int) {
	v := jitSSAValue{op: ssaCopyMemory, imm: from.Offset, offset: to.Offset, width: width, nargs: 2}
	v.args[0], v.args[1] = c.ssa.read(from.Reg), c.ssa.read(to.Reg)
	c.ssa.append(v)
	delete(c.ssa.registers, jitCopyScratch())
}

func (c *jitCodeBuilder) functionPrologue(frameSize, dataOffset int64) {
	c.dataOffset = dataOffset
	c.ssaValue(ssaFrameEnter, obj.Addr{}, frameSize, _RAX, _RSP)
	clear(c.ssa.registers)
	c.mov(_RAX, _R10)
	c.loadMem(_R10, dataOffset, _R12)
}

func (c *jitCodeBuilder) finishFunction(frameSize int64) {
	c.ssaValue(ssaFrameExit, obj.Addr{}, frameSize, _RSP)
}

func (c *jitCodeBuilder) restoreJITRegs() {
	c.loadJITContext(_R10)
	c.loadMem(_R10, c.dataOffset, _R12)
}

func (c *jitCodeBuilder) loadJITContext(to obj.Addr) { c.loadMem(_RSP, jitFrameContext, to) }

func (c *jitCodeBuilder) call(reg obj.Addr) {
	// All six ABI argument registers are implicit uses. Do not propagate their
	// copies away: the callee reads the physical argument registers.
	c.ssaValue(ssaCall, obj.Addr{}, 0, reg, _RAX, _RBX, _RCX, _RDI, _RSI, _R8)
	clear(c.ssa.registers)
}

func (c *jitCodeBuilder) ret() { c.ssaValue(ssaReturn, obj.Addr{}, 0, _RAX, _RSP) }

// optimizeJITSSA preserves instruction order. Value numbering uses memory
// versions for loads; any write or helper call invalidates previous loads.
// Available definitions, rather than just equal expressions, constrain copy
// propagation so a reused physical register can never resurrect an old value.
func optimizeJITSSA(input []jitSSAValue) ([]jitSSAValue, []bool, int) {
	values := slices.Clone(input)
	numbers := make([]int, len(values))
	available := make(map[int16]int)
	// Intrusive lists index definitions by value number. Looking up an equal
	// value visits only its copies, rather than scanning every live register.
	heads := make([]int, len(values))
	next := make([]int, len(values))
	type expression struct {
		op           jitSSAOp
		a, b, memory int
		imm          int64
	}
	expressions := make(map[expression]int)
	find := func(number int) (int, bool) {
		best := len(values)
		for link := heads[number]; link != 0; link = next[link-1] {
			id := link - 1
			if current, ok := available[values[id].reg]; ok && current == id && id < best {
				best = id
			}
		}
		return best, best != len(values)
	}
	folded := 0
	for id := range values {
		v := &values[id]
		// Effects with implicit operands must keep their original ABI homes.
		explicit := v.nargs
		switch v.op {
		case ssaCall:
			explicit = 1
		case ssaFrameEnter, ssaFrameExit, ssaReturn:
			explicit = 0
		}
		for i := 0; i < explicit; i++ {
			if replacement, ok := find(numbers[v.args[i]]); ok {
				v.args[i] = replacement
			}
		}
		switch v.op {
		case ssaAdd:
			arg := values[numbers[v.args[0]]]
			if arg.op == ssaConst {
				v.op, v.imm, v.nargs = ssaConst, arg.imm+v.imm, 0
				folded++
			} else if v.imm == 0 {
				v.op = ssaCopy
				folded++
			}
		case ssaXor:
			a, b := numbers[v.args[0]], numbers[v.args[1]]
			if a == b {
				v.op, v.imm, v.nargs = ssaConst, 0, 0
				folded++
			} else if values[a].op == ssaConst && values[b].op == ssaConst {
				v.op, v.imm, v.nargs = ssaConst, values[a].imm^values[b].imm, 0
				folded++
			}
		}
		numbers[id] = id
		if v.op == ssaCopy {
			numbers[id] = numbers[v.args[0]]
		}
		if v.op == ssaConst || v.op == ssaAdd || v.op == ssaXor || v.op == ssaLoad {
			key := expression{op: v.op, imm: v.imm, memory: v.memory}
			if v.nargs > 0 {
				key.a = numbers[v.args[0]]
			}
			if v.nargs > 1 {
				key.b = numbers[v.args[1]]
			}
			if previous, ok := expressions[key]; ok {
				numbers[id] = previous
				if replacement, ok := find(previous); ok {
					v.op, v.args[0], v.nargs = ssaCopy, replacement, 1
					v.imm, v.memory = 0, 0
					folded++
				}
			} else {
				expressions[key] = id
			}
		}
		if !v.effect() {
			available[v.reg] = id
			next[id] = heads[numbers[id]]
			heads[numbers[id]] = id + 1
		}
		switch v.op {
		case ssaCall, ssaFrameEnter:
			clear(available)
			clear(expressions)
		case ssaCopyMemory:
			delete(available, jitCopyScratch())
		}
	}
	// A reverse walk suffices because definitions precede uses in this block.
	live := make([]bool, len(values))
	for id := len(values) - 1; id >= 0; id-- {
		v := values[id]
		if !v.effect() && !live[id] {
			continue
		}
		live[id] = true
		for _, arg := range v.args[:v.nargs] {
			live[arg] = true
		}
		if v.memory != 0 {
			live[v.memory-1] = true
		}
	}
	return values, live, folded
}

func (c *jitCodeBuilder) lowerSSA() {
	// Count backend instructions before optimization without assembling/loading
	// another executable. The retained frame operations also build pcsp here.
	before := &jitCodeBuilder{}
	before.emitSSA(c.ssa.values, nil)
	values, live, folded := optimizeJITSSA(c.ssa.values)
	c.instructions, c.codeWords, c.pcsp = nil, nil, nil
	c.emitSSA(values, live)
	c.stats = jitCodegenStats{
		SSAValuesBefore:    len(c.ssa.values),
		InstructionsBefore: before.machineInstructionCount(),
		InstructionsAfter:  c.machineInstructionCount(),
		FoldedInstructions: folded,
	}
	for _, used := range live {
		if used {
			c.stats.SSAValuesAfter++
		}
	}
	c.stats.RemovedInstructions = c.stats.InstructionsBefore - c.stats.InstructionsAfter
}

func (c *jitCodeBuilder) emitSSA(values []jitSSAValue, live []bool) {
	for id, v := range values {
		if live != nil && !live[id] {
			continue
		}
		var args [7]obj.Addr
		for i, arg := range v.args[:v.nargs] {
			args[i] = registerAddr(int(values[arg].reg))
		}
		to := registerAddr(int(v.reg))
		switch v.op {
		case ssaInput:
		case ssaConst:
			c.machineMovConst(v.imm, to)
		case ssaCopy:
			if args[0] != to {
				c.machineMov(args[0], to)
			}
		case ssaAdd:
			c.machineLea(args[0], v.imm, to)
		case ssaXor:
			if args[1] == to {
				args[0], args[1] = args[1], args[0]
			}
			if args[0] != to {
				c.machineMov(args[0], to)
			}
			c.machineXor(args[1], to)
		case ssaLoad:
			c.machineLoadMem(args[0], v.imm, to)
		case ssaStore:
			dst := mem(args[1], v.imm)
			switch v.width {
			case 8:
				c.machineStore8(args[0], dst)
			case 16:
				c.machineStore16(args[0], dst)
			case 32:
				c.machineStore32(args[0], dst)
			case 64:
				c.machineMov(args[0], dst)
			}
		case ssaCopyMemory:
			if v.width == 32 {
				c.machineVectorMove32(mem(args[0], v.imm), mem(args[1], v.offset))
			} else {
				c.machineVectorMove16(mem(args[0], v.imm), mem(args[1], v.offset))
			}
		case ssaCall:
			c.machineCall(args[0])
		case ssaFrameEnter:
			c.machineFunctionPrologue(v.imm)
		case ssaFrameExit:
			c.machineFinishFunction(v.imm)
		case ssaReturn:
			c.machineRet()
		default:
			panic("JIT SSA: unsupported operation")
		}
	}
}
