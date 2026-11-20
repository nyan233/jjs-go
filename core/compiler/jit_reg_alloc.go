package compiler

import (
	"sort"

	"github.com/twitchyliquid64/golang-asm/obj/x86"
)

type jitLiveRange struct {
	Virtual int
	Start   int
	End     int
}

type jitAllocationResult struct {
	Registers map[int]int
	Spilled   []int
}

// allocateJITRegisters performs a linear-scan allocation over non-overlapping
// physical registers. Intervals include both endpoints.
func allocateJITRegisters(ranges []jitLiveRange, physical []int) jitAllocationResult {
	ordered := append([]jitLiveRange(nil), ranges...)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].Start == ordered[j].Start {
			return ordered[i].End < ordered[j].End
		}
		return ordered[i].Start < ordered[j].Start
	})

	type activeRange struct {
		rangeIndex int
		end        int
		register   int
	}
	active := make([]activeRange, 0, len(physical))
	free := append([]int(nil), physical...)
	result := jitAllocationResult{Registers: make(map[int]int, len(ordered))}

	for index, current := range ordered {
		for len(active) > 0 {
			earliest := 0
			for i := 1; i < len(active); i++ {
				if active[i].end < active[earliest].end {
					earliest = i
				}
			}
			if active[earliest].end >= current.Start {
				break
			}
			free = append(free, active[earliest].register)
			active = append(active[:earliest], active[earliest+1:]...)
		}
		if len(free) == 0 {
			result.Spilled = append(result.Spilled, current.Virtual)
			continue
		}
		register := free[len(free)-1]
		free = free[:len(free)-1]
		result.Registers[current.Virtual] = register
		active = append(active, activeRange{
			rangeIndex: index,
			end:        current.End,
			register:   register,
		})
	}
	return result
}

var jitAllocatableRegisters = []int{
	x86.REG_R13,
	x86.REG_R15,
	x86.REG_R9,
}
