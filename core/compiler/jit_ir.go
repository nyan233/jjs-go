//go:build amd64 && !arm64_asm_test

package compiler

import (
	"fmt"
	"slices"

	"github.com/twitchyliquid64/golang-asm/obj"
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

func (c *jitCodeBuilder) prepareMachineCode() ([]*obj.Prog, jitCodegenStats, error) {
	c.lowerSSA()
	stats := c.stats
	instructions := c.instructions

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
	return instructions, stats, nil
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
	slices.Sort(virtuals)
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
