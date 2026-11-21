//go:build amd64 && !arm64_asm_test

package compiler

import (
	"fmt"
	"sort"

	"github.com/twitchyliquid64/golang-asm/obj"
	"github.com/twitchyliquid64/golang-asm/obj/x86"
)

const jitVirtualRegisterBase = 20000

func (c *jitCodeBuilder) newVirtualRegister() obj.Addr {
	register := jitVirtualRegisterBase + c.virtualCount
	c.virtualCount++
	return obj.Addr{Type: obj.TYPE_REG, Reg: int16(register)}
}

func isJITVirtualRegister(address obj.Addr) bool {
	return (address.Type == obj.TYPE_REG || address.Type == obj.TYPE_MEM) &&
		address.Reg >= jitVirtualRegisterBase
}

func (c *jitCodeBuilder) prepareIR() ([]*obj.Prog, jitCodegenStats, error) {
	stats := jitCodegenStats{InstructionsBefore: len(c.instructions)}
	instructions, optimized := optimizeJITIR(c.instructions)
	stats.RemovedInstructions = optimized.removed
	stats.FoldedInstructions = optimized.folded
	stats.BranchesRemoved = optimized.branches

	ranges := jitVirtualLiveRanges(instructions)
	stats.VirtualRegisters = len(ranges)
	allocation := allocateJITRegisters(ranges, jitAllocatableRegisters)
	stats.SpilledRegisters = len(allocation.Spilled)
	if stats.SpilledRegisters != 0 {
		return nil, stats, fmt.Errorf("json: JIT register allocation spilled %d virtual registers", stats.SpilledRegisters)
	}
	physical := make(map[int]struct{}, len(allocation.Registers))
	for _, register := range allocation.Registers {
		physical[register] = struct{}{}
	}
	stats.RegisterReuse = len(ranges) - len(physical)
	rewriteJITVirtualRegisters(instructions, allocation.Registers)
	stats.InstructionsAfter = len(instructions)
	return instructions, stats, nil
}

type jitOptimizationStats struct {
	removed  int
	folded   int
	branches int
}

func optimizeJITIR(input []*obj.Prog) ([]*obj.Prog, jitOptimizationStats) {
	output := make([]*obj.Prog, 0, len(input))
	var stats jitOptimizationStats
	for index, current := range input {
		if current.As == x86.AMOVQ &&
			current.From.Type == obj.TYPE_REG &&
			current.To.Type == obj.TYPE_REG &&
			current.From.Reg == current.To.Reg {
			stats.removed++
			continue
		}
		if target, ok := current.To.Val.(*obj.Prog); current.As == obj.AJMP &&
			current.To.Type == obj.TYPE_BRANCH &&
			ok &&
			index+1 < len(input) &&
			target == input[index+1] {
			stats.removed++
			stats.branches++
			continue
		}
		if len(output) > 0 {
			previous := output[len(output)-1]
			if current.From.Type == obj.TYPE_REG &&
				current.To.Type == obj.TYPE_MEM &&
				previous.As == x86.AMOVQ &&
				previous.From.Type == obj.TYPE_CONST &&
				previous.To == current.From &&
				(current.As == x86.AMOVL || current.As == x86.AMOVW || current.As == x86.AMOVB) {
				current.From = previous.From
				output = output[:len(output)-1]
				stats.folded++
			}
			if isJITImmediateArithmetic(previous) &&
				isJITImmediateArithmetic(current) &&
				previous.To == current.To {
				delta, ok := combineJITImmediate(previous, current)
				if ok {
					if delta == 0 {
						output = output[:len(output)-1]
					} else {
						previous.As = x86.AADDQ
						previous.From.Offset = delta
					}
					stats.folded++
					continue
				}
			}
			if current.As == x86.AMOVQ &&
				previous.As == x86.AMOVQ &&
				current.From.Type == obj.TYPE_CONST &&
				previous.From.Type == obj.TYPE_CONST &&
				current.To == previous.To {
				output = output[:len(output)-1]
				stats.removed++
			}
		}
		output = append(output, current)
	}
	return output, stats
}

func isJITImmediateArithmetic(instruction *obj.Prog) bool {
	return (instruction.As == x86.AADDQ || instruction.As == x86.ASUBQ) &&
		instruction.From.Type == obj.TYPE_CONST &&
		instruction.To.Type == obj.TYPE_REG
}

func combineJITImmediate(left, right *obj.Prog) (int64, bool) {
	leftValue := left.From.Offset
	if left.As == x86.ASUBQ {
		leftValue = -leftValue
	}
	rightValue := right.From.Offset
	if right.As == x86.ASUBQ {
		rightValue = -rightValue
	}
	if rightValue > 0 && leftValue > int64(^uint64(0)>>1)-rightValue {
		return 0, false
	}
	if rightValue < 0 && leftValue < -int64(^uint64(0)>>1)-1-rightValue {
		return 0, false
	}
	return leftValue + rightValue, true
}

func jitVirtualLiveRanges(instructions []*obj.Prog) []jitLiveRange {
	start := make(map[int]int)
	end := make(map[int]int)
	for position, instruction := range instructions {
		trackJITVirtualAddress(instruction.From, position, start, end)
		trackJITVirtualAddress(instruction.To, position, start, end)
	}
	virtuals := make([]int, 0, len(start))
	for virtual := range start {
		virtuals = append(virtuals, virtual)
	}
	sort.Ints(virtuals)
	ranges := make([]jitLiveRange, 0, len(virtuals))
	for _, virtual := range virtuals {
		ranges = append(ranges, jitLiveRange{
			Virtual: virtual,
			Start:   start[virtual],
			End:     end[virtual],
		})
	}
	return ranges
}

func trackJITVirtualAddress(address obj.Addr, position int, start, end map[int]int) {
	if !isJITVirtualRegister(address) {
		return
	}
	virtual := int(address.Reg)
	if _, ok := start[virtual]; !ok {
		start[virtual] = position
	}
	end[virtual] = position
}

func rewriteJITVirtualRegisters(instructions []*obj.Prog, registers map[int]int) {
	for _, instruction := range instructions {
		rewriteJITVirtualAddress(&instruction.From, registers)
		rewriteJITVirtualAddress(&instruction.To, registers)
	}
}

func rewriteJITVirtualAddress(address *obj.Addr, registers map[int]int) {
	if !isJITVirtualRegister(*address) {
		return
	}
	address.Reg = int16(registers[int(address.Reg)])
}
