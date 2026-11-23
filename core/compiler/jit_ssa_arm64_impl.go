//go:build arm64 || arm64_asm_test

package compiler

import (
	"sync"

	"github.com/twitchyliquid64/golang-asm/obj"
)

// This is machine SSA: each GPR, NZCV and memory definition has a unique
// (node, result) identity. Lazy block parameters are joined by phi nodes,
// including loop backedges. Physical homes stay fixed; no values move across
// calls and no stores (in particular GC roots) are removed. Unknown operations
// and embedded SIMD kernels read/define the entire state and remain opaque.
const (
	ssaFlags     = 32
	ssaMemory    = 33
	ssaFloat     = 34
	ssaRegisters = 35
)

type armSSARef uint32

func (r armSSARef) node() int { return int(r >> 6) }

type armSSAOp uint8

const (
	armOpaque armSSAOp = iota
	armInput
	armPhi
	armConst
	armInsert
	armCopy
	armAddImm
	armSubImm
	armAdd
	armSub
	armXor
	armAnd
	armShift
	armLoad
	armStore
	armCompare
	armCompareImm
	armBranch
	armJump
	armReturn
	armPure
	armCall
)

type armSSANode struct {
	pc, end    int32
	word       uint32
	arg, count int32
	block      int32
	op         armSSAOp
	dst        uint8
	effect     bool
}

type armSSABlock struct {
	start, end, pred int
	entry, state     [ssaRegisters]armSSARef
}

type armSSAPred struct{ block, next int }

type armSSAFact struct {
	value uint64
	state uint8
}

type armSSA struct {
	aliases                 []armSSARef
	nodes                   []armSSANode
	args                    []armSSARef
	blocks                  []armSSABlock
	preds                   []armSSAPred
	leaders                 []bool
	blockAt, targets, remap []int
	facts                   []armSSAFact
	live                    []bool
	work                    []int
}

var armSSAPool = sync.Pool{New: func() any { return new(armSSA) }}

func ssaResize[T any](s []T, n int) []T {
	if cap(s) < n {
		return make([]T, n)
	}
	s = s[:n]
	clear(s)
	return s
}

func (s *armSSA) append(n armSSANode) armSSARef {
	s.nodes = append(s.nodes, n)
	return armSSARef((len(s.nodes)-1)<<6 | int(n.dst))
}

func (s *armSSA) read(block, reg int) armSSARef {
	if ref := s.blocks[block].state[reg]; ref != 0 {
		return ref
	}
	op := armPhi
	if s.blocks[block].pred < 0 {
		op = armInput
	}
	ref := s.append(armSSANode{pc: -1, end: -1, block: int32(block), op: op, dst: uint8(reg)})
	s.blocks[block].entry[reg] = ref
	s.blocks[block].state[reg] = ref
	return ref
}

func armBranchTarget(word uint32, pc int) (int, bool) {
	if word&0xfc000000 == 0x14000000 {
		return pc + int(int32(word<<6)>>6), true
	}
	if word&0xff000010 == 0x54000000 {
		return pc + int(int32(word<<8)>>13), true
	}
	return 0, false
}

// Return explicit register operands; -1 denotes XZR, distinct from SP (31).
func decodeArmSSA(w uint32) (armSSAOp, int, [3]int, int) {
	d, a, b := int(w&31), int(w>>5&31), int(w>>16&31)
	zero := func(r int) int {
		if r == 31 {
			return -1
		}
		return r
	}
	switch {
	case w&0xff800000 == 0xd2800000 && d != 31:
		return armConst, d, [3]int{}, 0
	case w&0xff800000 == 0xf2800000 && d != 31:
		return armInsert, d, [3]int{d}, 1
	case w&0xffe0ffe0 == 0xaa0003e0 && d != 31:
		return armCopy, d, [3]int{zero(b)}, 1
	case w&0xff800000 == 0x91000000:
		return armAddImm, d, [3]int{a}, 1
	case w&0xff800000 == 0xd1000000:
		return armSubImm, d, [3]int{a}, 1
	case w&0xffe0fc00 == 0x8b000000 && d != 31:
		return armAdd, d, [3]int{zero(a), zero(b)}, 2
	case w&0xffe0fc00 == 0xcb000000 && d != 31:
		return armSub, d, [3]int{zero(a), zero(b)}, 2
	case w&0xffe0fc00 == 0xca000000 && d != 31:
		return armXor, d, [3]int{zero(a), zero(b)}, 2
	case w&0xffe0fc00 == 0x8a000000 && d != 31:
		return armAnd, d, [3]int{zero(a), zero(b)}, 2
	case w&0xffc0fc00 == 0xd340fc00 && d != 31:
		return armShift, d, [3]int{zero(a)}, 1
	case (w&0xffe0fc00 == 0x9ac00800 || w&0xffe0fc00 == 0x9bc07c00) && d != 31:
		return armPure, d, [3]int{zero(a), zero(b)}, 2
	case w&0xffe08000 == 0x9b008000 && d != 31:
		return armPure, d, [3]int{zero(a), zero(b), zero(int(w >> 10 & 31))}, 3
	case w&0xffc0001f == 0xfd400000 || w&0xffc0001f == 0xbd400000:
		return armPure, ssaFloat, [3]int{a, ssaMemory}, 2
	case w == 0x1e22c000:
		return armPure, ssaFloat, [3]int{ssaFloat}, 1
	case w&0xffffffe0 == 0x9e660000 && d != 31:
		return armPure, d, [3]int{ssaFloat}, 1
	case w&0xffe0fc1f == 0xeb00001f:
		return armCompare, ssaFlags, [3]int{zero(a), zero(b)}, 2
	case w&0xff80001f == 0xf100001f:
		return armCompareImm, ssaFlags, [3]int{a}, 1
	case w&0xff000010 == 0x54000000:
		return armBranch, 0, [3]int{ssaFlags}, 1
	case w&0xfc000000 == 0x14000000:
		return armJump, 0, [3]int{}, 0
	case w == 0xd65f03c0:
		return armReturn, 0, [3]int{}, 0
	}
	if d != 31 {
		switch w & 0xffc00000 {
		case 0xf9400000, 0x39400000, 0x79400000, 0xb9400000, 0x39800000, 0x79800000, 0xb9800000:
			return armLoad, d, [3]int{a, ssaMemory}, 2
		}
		if w&0xffe00c00 == 0xf8400000 {
			return armLoad, d, [3]int{a, ssaMemory}, 2
		}
	}
	switch w & 0xffc00000 {
	case 0xf9000000, 0x39000000, 0x79000000, 0xb9000000:
		return armStore, ssaMemory, [3]int{zero(d), a, ssaMemory}, 3
	}
	switch w & 0xffe00c00 {
	case 0xf8000000, 0x38000000, 0x78000000, 0xb8000000:
		return armStore, ssaMemory, [3]int{zero(d), a, ssaMemory}, 3
	}
	return armOpaque, 0, [3]int{}, 0
}

func (s *armSSA) build(words []uint32, opaque []jitCodeRange, calls []jitKnownCall) bool {
	n := len(words)
	s.nodes = s.nodes[:0]
	s.args = s.args[:0]
	s.blocks = s.blocks[:0]
	s.preds = s.preds[:0]
	s.nodes = append(s.nodes, armSSANode{pc: -1, op: armConst}) // XZR
	s.leaders = ssaResize(s.leaders, n+1)
	s.blockAt = ssaResize(s.blockAt, n+1)
	s.targets = ssaResize(s.targets, n)
	for i := range s.targets {
		s.targets[i] = -1
	}
	s.leaders[0], s.leaders[n] = true, true
	span := 0
	for pc := 0; pc < n; pc++ {
		if span < len(opaque) && opaque[span].start == pc {
			pc = opaque[span].end - 1
			span++
			continue
		}
		w := words[pc]
		if target, ok := armBranchTarget(w, pc); ok {
			if target < 0 || target >= n {
				return false
			}
			s.targets[pc] = target
			s.leaders[target], s.leaders[pc+1] = true, true
		} else if w == 0xd65f03c0 {
			s.leaders[pc+1] = true
		} else if (w>>25&15 == 10 || w>>25&15 == 11) && w&0xfffffc1f != 0xd63f0000 {
			// Unknown control flow cannot be safely relocated as an opaque word.
			return false
		}
	}
	for _, span := range opaque {
		for pc := span.start + 1; pc < span.end; pc++ {
			if s.leaders[pc] {
				return false
			}
		}
	}
	for pc := 0; pc < n; pc++ {
		if s.leaders[pc] {
			if len(s.blocks) > 0 {
				s.blocks[len(s.blocks)-1].end = pc
			}
			s.blocks = append(s.blocks, armSSABlock{start: pc, end: n, pred: -1})
		}
		s.blockAt[pc] = len(s.blocks) - 1
	}
	addPred := func(to, from int) {
		s.preds = append(s.preds, armSSAPred{from, s.blocks[to].pred})
		s.blocks[to].pred = len(s.preds) - 1
	}
	for b := range s.blocks {
		pc := s.blocks[b].end - 1
		if target := s.targets[pc]; target >= 0 {
			addPred(s.blockAt[target], b)
		}
		if b+1 < len(s.blocks) && words[pc]&0xfc000000 != 0x14000000 && words[pc] != 0xd65f03c0 {
			addPred(b+1, b)
		}
	}
	span = 0
	call := 0
	for b := range s.blocks {
		for pc := s.blocks[b].start; pc < s.blocks[b].end; {
			end := pc + 1
			op, dst, regs, count := decodeArmSSA(words[pc])
			if span < len(opaque) && opaque[span].start == pc {
				end, op = opaque[span].end, armOpaque
				span++
			}
			if call < len(calls) && calls[call].pc == pc {
				op = armCall
			}
			node := armSSANode{pc: int32(pc), end: int32(end), word: words[pc], op: op, dst: uint8(dst), block: int32(b), arg: int32(len(s.args))}
			switch op {
			case armCall:
				node.effect = true
				for r := 0; r < ssaRegisters; r++ {
					if calls[call].reads&(1<<r) != 0 {
						s.args = append(s.args, s.read(b, r))
					}
				}
				call++
			case armOpaque:
				node.effect = true
				for r := 0; r < ssaRegisters; r++ {
					s.args = append(s.args, s.read(b, r))
				}
			case armReturn:
				node.effect = true
				for _, r := range [...]int{0, 29, 30, 31} {
					s.args = append(s.args, s.read(b, r))
				}
			default:
				node.effect = op == armStore || op == armBranch || op == armJump || (dst == 31 && (op == armAddImm || op == armSubImm))
				for _, r := range regs[:count] {
					var ref armSSARef
					if r >= 0 {
						ref = s.read(b, r)
					}
					s.args = append(s.args, ref)
				}
			}
			node.count = int32(len(s.args)) - node.arg
			ref := s.append(node)
			if op == armOpaque || op == armCall {
				for r := 0; r < ssaRegisters; r++ {
					s.blocks[b].state[r] = ref&^63 | armSSARef(r)
				}
			} else if op != armBranch && op != armJump && op != armReturn {
				s.blocks[b].state[dst] = ref
			}
			pc = end
		}
	}
	// Missing predecessor definitions become pass-through phis, so this loop
	// can append nodes. Backedges see the already-created placeholder identity.
	for i := 1; i < len(s.nodes); i++ {
		if s.nodes[i].op != armPhi {
			continue
		}
		b, reg := int(s.nodes[i].block), int(s.nodes[i].dst)
		start := len(s.args)
		if b == 0 {
			// A backedge into the entry block still has the external function
			// entry as a predecessor. Its initial registers are not loop values.
			input := s.append(armSSANode{pc: -1, end: -1, op: armInput, dst: uint8(reg)})
			s.args = append(s.args, input)
		}
		for p := s.blocks[b].pred; p >= 0; p = s.preds[p].next {
			s.args = append(s.args, s.read(s.preds[p].block, reg))
		}
		s.nodes[i].arg, s.nodes[i].count = int32(start), int32(len(s.args)-start)
	}
	return true
}

func (s *armSSA) operands(id int) []armSSARef {
	n := s.nodes[id]
	return s.args[n.arg : n.arg+n.count]
}

const armNoAlias = ^armSSARef(0)

func (s *armSSA) canonical(r armSSARef) armSSARef {
	original := r
	for steps := 0; steps < 128 && r != 0; steps++ {
		next := s.aliases[r.node()]
		if next == armNoAlias || next == r {
			if original != r {
				s.aliases[original.node()] = r
			}
			return r
		}
		r = next
	}
	if r == 0 && original != 0 {
		s.aliases[original.node()] = 0
		return 0
	}
	return original // Cyclic unreachable phis remain conservative.
}

func (s *armSSA) numberValues() {
	s.aliases = ssaResize(s.aliases, len(s.nodes))
	for i, n := range s.nodes {
		s.aliases[i] = armNoAlias
		if n.op == armCopy {
			s.aliases[i] = s.args[n.arg]
		}
	}
	// Most phis simply pass through an untouched register. Collapse these first,
	// then joins whose incoming values become identical after copy propagation.
	for pass := 0; pass < 2; pass++ {
		for id, n := range s.nodes {
			if n.op != armPhi {
				continue
			}
			ref := armSSARef(id<<6 | int(n.dst))
			same, valid, conflict := armSSARef(0), false, false
			for _, arg := range s.operands(id) {
				arg = s.canonical(arg)
				if arg == ref {
					continue
				}
				if valid && same != arg {
					conflict = true
					break
				}
				same, valid = arg, true
			}
			if valid && !conflict {
				s.aliases[id] = same
			}
		}
	}
	for i, n := range s.nodes {
		s.canonical(armSSARef(i<<6 | int(n.dst)))
	}
}

func armImmediate(w uint32) uint64 { return uint64(w>>10&4095) << (12 * (w >> 22 & 1)) }

func armCompareFlags(a, b uint64) uint64 {
	r := a - b
	flags := (r >> 63) << 3
	if r == 0 {
		flags |= 4
	}
	if a >= b {
		flags |= 2
	}
	if (a^b)&(a^r)>>63 != 0 {
		flags |= 1
	}
	return flags
}

func (s *armSSA) constant(r armSSARef) (uint64, bool) {
	if r == 0 {
		return 0, true
	}
	id := r.node()
	if f := s.facts[id]; f.state != 0 {
		return f.value, f.state == 2
	}
	s.facts[id].state = 1 // Visiting/unknown; cycles are not speculatively folded.
	n := s.nodes[id]
	args := s.operands(id)
	var value uint64
	known := false
	a, ka, b, kb := uint64(0), false, uint64(0), false
	if len(args) > 0 {
		a, ka = s.constant(args[0])
	}
	if len(args) > 1 {
		b, kb = s.constant(args[1])
	}
	switch n.op {
	case armConst:
		value, known = uint64(n.word>>5&65535)<<(16*(n.word>>21&3)), true
	case armPhi:
		value, known = a, ka && len(args) > 0
		for _, arg := range args[1:] {
			v, k := s.constant(arg)
			known = known && k && value == v
		}
	case armCopy:
		value, known = a, ka
	case armInsert:
		shift := 16 * (n.word >> 21 & 3)
		value, known = a&^(uint64(65535)<<shift)|uint64(n.word>>5&65535)<<shift, ka
	case armAddImm:
		value, known = a+armImmediate(n.word), ka
	case armSubImm:
		value, known = a-armImmediate(n.word), ka
	case armAdd:
		value, known = a+b, ka && kb
	case armSub:
		value, known = a-b, ka && kb
	case armXor:
		value, known = a^b, ka && kb
	case armAnd:
		value, known = a&b, ka && kb
	case armShift:
		value, known = a>>(n.word>>16&63), ka
	case armCompare:
		value, known = armCompareFlags(a, b), ka && kb
	case armCompareImm:
		value, known = armCompareFlags(a, armImmediate(n.word)), ka
	}
	if known {
		s.facts[id] = armSSAFact{value, 2}
	}
	return value, known
}

func armOneWordConstant(value uint64, reg uint8) (uint32, bool) {
	for shift := uint32(0); shift < 64; shift += 16 {
		if value&^(uint64(65535)<<shift) == 0 {
			return 0xd2800000 | uint32(value>>shift)<<5 | shift/16<<21 | uint32(reg), true
		}
	}
	return 0, false
}

func armMemoryOffset(w uint32) int64 {
	if w&0x01000000 != 0 {
		return int64(w>>10&4095) << (w >> 30)
	}
	return int64(int32(w<<11) >> 23)
}

// Rewrite only explicit operands whose equivalent value still has a live
// physical home at this instruction. ABI operands, NZCV, SP and FP operands
// keep their prescribed homes. SSA equality alone is not sufficient: an old
// definition's register may already have been overwritten.
func (s *armSSA) propagate(id int, homes [ssaRegisters]armSSARef) bool {
	n := &s.nodes[id]
	var shifts [3]uint
	count := 0
	switch n.op {
	case armCopy:
		shifts[0], count = 16, 1
	case armAddImm, armSubImm, armShift, armLoad, armCompareImm:
		shifts[0], count = 5, 1
	case armAdd, armSub, armXor, armAnd, armCompare:
		shifts, count = [3]uint{5, 16}, 2
	case armStore:
		shifts, count = [3]uint{0, 5}, 2
	case armPure:
		switch {
		case n.word&0xffe0fc00 == 0x9ac00800 || n.word&0xffe0fc00 == 0x9bc07c00:
			shifts, count = [3]uint{5, 16}, 2
		case n.word&0xffe08000 == 0x9b008000:
			shifts, count = [3]uint{5, 16, 10}, 3
		case n.word&0xffc0001f == 0xfd400000 || n.word&0xffc0001f == 0xbd400000:
			shifts[0], count = 5, 1
		}
	}
	changed := false
	for i, shift := range shifts[:count] {
		if n.word>>shift&31 == 31 {
			continue
		}
		arg := s.args[int(n.arg)+i]
		number := s.canonical(arg)
		best := arg
		reg := -1
		for r := 0; r < 31; r++ {
			if homes[r] != 0 && homes[r].node() < best.node() && s.canonical(homes[r]) == number {
				best, reg = homes[r], r
			}
		}
		if reg >= 0 {
			s.args[int(n.arg)+i] = best
			n.word = n.word&^(31<<shift) | uint32(reg)<<shift
			changed = true
		}
	}
	return changed
}

func armConstantCondition(condition uint32, flags uint64) (bool, bool) {
	switch condition {
	case 0:
		return flags&4 != 0, true
	case 1:
		return flags&4 == 0, true
	case 2:
		return flags&2 != 0, true
	case 3:
		return flags&2 == 0, true
	case 4:
		return flags&8 != 0, true
	case 5:
		return flags&8 == 0, true
	default:
		return false, false
	}
}

func (s *armSSA) optimize() (int, int) {
	s.numberValues()
	s.facts = ssaResize(s.facts, len(s.nodes))
	for i := 1; i < len(s.nodes); i++ {
		s.constant(armSSARef(i<<6 | int(s.nodes[i].dst)))
	}
	folded, branches := 0, 0
	block := -1
	var homes [ssaRegisters]armSSARef
	for id := 1; id < len(s.nodes); id++ {
		n := &s.nodes[id]
		if n.pc < 0 {
			continue
		}
		if int(n.block) != block {
			block = int(n.block)
			homes = s.blocks[block].entry
		}
		ref := armSSARef(id<<6 | int(n.dst))
		if n.op == armOpaque || n.op == armCall {
			for r := range homes {
				homes[r] = ref&^63 | armSSARef(r)
			}
			continue
		}
		if n.op == armBranch {
			flags := s.facts[s.args[n.arg].node()]
			if flags.state == 2 {
				take, supported := armConstantCondition(n.word&15, flags.value)
				if supported {
					target, _ := armBranchTarget(n.word, int(n.pc))
					n.count = 0
					if take {
						n.op, n.word = armJump, 0x14000000|uint32(target-int(n.pc))&0x3ffffff
					} else {
						n.word, n.effect = 0, false
					}
					branches++
				}
			}
		}
		if n.op == armJump {
			if target, _ := armBranchTarget(n.word, int(n.pc)); target == int(n.end) {
				n.word, n.effect = 0, false
				branches++
			}
		}
		if n.op == armBranch || n.op == armJump || n.op == armReturn {
			continue
		}
		if s.propagate(id, homes) {
			folded++
		}
		if n.dst < 31 && s.facts[id].state == 2 {
			value := s.facts[id].value
			if old := homes[n.dst]; old != 0 && s.facts[old.node()].state == 2 && s.facts[old.node()].value == value {
				n.word, n.op, n.arg, n.count = 0, armCopy, int32(len(s.args)), 1
				s.args = append(s.args, old)
				s.aliases[id] = old
				folded++
			} else if word, ok := armOneWordConstant(value, n.dst); ok && word != n.word {
				n.word, n.op, n.count = word, armConst, 0
				folded++
			}
		} else if n.op == armLoad {
			args := s.operands(id)
			base, memory := s.canonical(args[0]), s.canonical(args[1])
			var replacement armSSARef
			found := false
			store := s.nodes[memory.node()]
			if n.word>>30 == 3 && store.op == armStore && store.word>>30 == 3 && armMemoryOffset(n.word) == armMemoryOffset(store.word) {
				stored := s.operands(memory.node())
				if base == s.canonical(stored[1]) {
					replacement, found = s.canonical(stored[0]), true
				}
			}
			for r := 0; r < 31; r++ {
				if homes[r] == 0 {
					continue
				}
				candidate := s.canonical(homes[r])
				load := s.nodes[candidate.node()]
				match := found && candidate == replacement
				if load.op == armLoad && load.word&^1023 == n.word&^1023 {
					args := s.operands(candidate.node())
					match = match || s.canonical(args[0]) == base && s.canonical(args[1]) == memory
				}
				if !match {
					continue
				}
				n.word = 0xaa0003e0 | uint32(r)<<16 | uint32(n.dst)
				if r == int(n.dst) {
					n.word = 0
				}
				n.op, n.arg, n.count = armCopy, int32(len(s.args)), 1
				s.args = append(s.args, homes[r])
				s.aliases[id] = homes[r]
				folded++
				break
			}
		}
		homes[n.dst] = ref
	}
	return folded, branches
}

func (s *armSSA) markLive() {
	s.live = ssaResize(s.live, len(s.nodes))
	s.work = s.work[:0]
	for i, n := range s.nodes {
		if n.effect {
			s.work = append(s.work, i)
		}
	}
	for len(s.work) > 0 {
		id := s.work[len(s.work)-1]
		s.work = s.work[:len(s.work)-1]
		if s.live[id] {
			continue
		}
		s.live[id] = true
		for _, arg := range s.operands(id) {
			if !s.live[arg.node()] {
				s.work = append(s.work, arg.node())
			}
		}
	}
}

func (c *jitCodeBuilder) optimizeARM64SSA() {
	s := armSSAPool.Get().(*armSSA)
	defer armSSAPool.Put(s)
	if !s.build(c.codeWords, c.opaqueRanges, c.knownCalls) {
		return
	}
	c.stats.SSABlocks, c.stats.SSAValues = len(s.blocks), len(s.nodes)-1
	for _, n := range s.nodes {
		if n.op == armPhi {
			c.stats.SSAPhis++
		}
	}
	c.stats.FoldedInstructions, c.stats.BranchesRemoved = s.optimize()
	s.markLive()
	s.remap = ssaResize(s.remap, len(c.codeWords)+1)
	out := c.codeWords[:0]
	for id, n := range s.nodes {
		if n.pc < 0 {
			continue
		}
		for pc := n.pc; pc < n.end; pc++ {
			s.remap[pc] = len(out) + int(pc-n.pc)
		}
		if s.live[id] && (n.word != 0 || n.op == armOpaque) {
			if n.op == armOpaque || n.op == armCall {
				out = append(out, c.codeWords[n.pc:n.end]...)
			} else {
				out = append(out, n.word)
			}
		} else {
			s.remap[n.pc] = len(out)
		}
	}
	s.remap[len(c.codeWords)] = len(out)
	// All non-opaque relative branches use the old-to-new PC map. No branch
	// enters an opaque kernel; its internal displacements remain unchanged.
	for id, n := range s.nodes {
		if n.pc < 0 || !s.live[id] || n.word == 0 || n.op == armOpaque {
			continue
		}
		if target, ok := armBranchTarget(n.word, int(n.pc)); ok {
			pc := s.remap[n.pc]
			delta := s.remap[target] - pc
			if n.op == armJump {
				out[pc] = n.word&0xfc000000 | uint32(delta)&0x3ffffff
			} else {
				if delta < -(1<<18) || delta >= 1<<18 {
					panic("ARM64 SSA branch out of range")
				}
				out[pc] = n.word&^0x00ffffe0 | (uint32(delta)&0x7ffff)<<5
			}
		}
	}
	for i := range c.pcsp {
		c.pcsp[i].PC = uint32(s.remap[c.pcsp[i].PC/4] * 4)
	}
	c.stats.RemovedInstructions = len(c.codeWords) - len(out)
	c.codeWords = out
}

// Counts describe the actual Go helper signatures, not all potential ABI
// argument registers. Tests check this table against the function types.
var armEncoderArgs = [jitHelperCount]int{
	jitHelperString: 3, jitHelperInt: 4, jitHelperFloat: 3, jitHelperBool: 2,
	jitHelperNull: 1, jitHelperPointer: 3, jitHelperSlice: 4, jitHelperInterface: 3,
	jitHelperReserve: 2, jitHelperByteSlice: 2, jitHelperResolvedField: 3,
	jitHelperCustomMarshal: 4, jitHelperFieldPrefix: 2, jitHelperFloatFormat: 6,
	jitHelperYield: 0, jitHelperMapStart: 3, jitHelperMapNext: 1, jitHelperMapEnd: 1,
	jitHelperValue: 3, jitHelperQuote: 4, jitHelperQuoteTail: 3,
}

func (c *jitCodeBuilder) callWithABI(reg obj.Addr, count int, floating bool) {
	if jitSSAEnabled && c.encoderFrame {
		reads := uint64(1)<<count - 1 | 1<<arm64Register(reg) | 1<<26 | 1<<28 | 1<<29 | 1<<31
		if floating {
			reads |= 1 << ssaFloat
		}
		c.knownCalls = append(c.knownCalls, jitKnownCall{len(c.codeWords), reads})
	}
	c.call(reg)
}
func (c *jitCodeBuilder) callEncoderHelper(index int) {
	c.callWithABI(_R11, armEncoderArgs[index], index == jitHelperFloatFormat)
}
func (c *jitCodeBuilder) callFloatFormatter() { c.callWithABI(_RT0, 6, true) }
