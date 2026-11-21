//go:build amd64 && !arm64_asm_test

package compiler

import "testing"

func TestJITLinearScanAllocator(t *testing.T) {
	result := allocateJITRegisters([]jitLiveRange{
		{Virtual: 0, Start: 0, End: 3},
		{Virtual: 1, Start: 1, End: 2},
		{Virtual: 2, Start: 4, End: 6},
	}, []int{10, 20})
	if len(result.Registers) != 3 || len(result.Spilled) != 0 {
		t.Fatalf("unexpected allocation: %#v", result)
	}
	if result.Registers[0] == result.Registers[1] {
		t.Fatal("overlapping virtual registers received the same physical register")
	}
	if result.Registers[0] != result.Registers[2] {
		t.Fatal("non-overlapping intervals did not reuse a register")
	}
}

func TestJITLinearScanAllocatorSpill(t *testing.T) {
	result := allocateJITRegisters([]jitLiveRange{
		{Virtual: 0, Start: 0, End: 5},
		{Virtual: 1, Start: 1, End: 5},
		{Virtual: 2, Start: 2, End: 5},
	}, []int{10})
	if len(result.Spilled) == 0 {
		t.Fatal("expected at least one spilled interval")
	}
}
