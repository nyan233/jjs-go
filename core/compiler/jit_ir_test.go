//go:build amd64 && !arm64_asm_test

package compiler

import (
	"reflect"
	"testing"
)

type codegenStringer interface {
	String() string
}

type codegenInterfaceFixture struct {
	Value codegenStringer
}

func TestJITSSALowersFoldedConstantsAMD64(t *testing.T) {
	code := newJITCodeBuilder("test")
	code.mov(_R9, _R9)
	code.movConst(10, _RAX)
	code.addConst(5, _RAX)
	code.subConst(3, _RAX)
	code.movConst(0, _R11)
	code.movConst(1, _R11)
	code.ret()

	instructions, stats, err := code.prepareMachineCode()
	if err != nil {
		t.Fatal(err)
	}
	if len(instructions) != 2 {
		t.Fatalf("instruction count = %d, want 2", len(instructions))
	}
	if stats.RemovedInstructions < 4 || stats.FoldedInstructions < 2 {
		t.Fatalf("unexpected optimization stats: %#v", stats)
	}
	if instructions[0].From.Offset != 12 || instructions[0].To.Reg != _RAX.Reg {
		t.Fatalf("folded instruction has unexpected operands: %#v", instructions[0])
	}
}

func TestJITVirtualRegistersReuseNonOverlappingRanges(t *testing.T) {
	code := newJITCodeBuilder("test")
	first := code.newVirtualRegister()
	code.mov(_RAX, first)
	code.movConst(7, _RAX)
	code.mov(first, mem(_RSP, 0))
	second := code.newVirtualRegister()
	code.mov(_RAX, second)
	code.movConst(8, _RAX)
	code.mov(second, mem(_RSP, 8))
	code.ret()

	instructions, stats, err := code.prepareMachineCode()
	if err != nil {
		t.Fatal(err)
	}
	if stats.VirtualRegisters != 2 {
		t.Fatalf("virtual registers = %d, want 2", stats.VirtualRegisters)
	}
	if stats.RegisterReuse != 1 {
		t.Fatalf("register reuse = %d, want 1", stats.RegisterReuse)
	}
	if stats.SpilledRegisters != 0 {
		t.Fatalf("unexpected spills: %d", stats.SpilledRegisters)
	}
	for _, instruction := range instructions {
		if isJITVirtualRegister(instruction.From) || isJITVirtualRegister(instruction.To) {
			t.Fatalf("virtual register was not rewritten: %#v", instruction)
		}
	}
}

func TestCodegenStatsAreReported(t *testing.T) {
	encoder, err := EncoderCodegenStats(reflect.TypeOf(TestJITNested{}))
	if err != nil {
		t.Fatal(err)
	}
	if encoder.Functions == 0 || encoder.InstructionsBefore == 0 || encoder.CodeBytes == 0 {
		t.Fatalf("incomplete encoder stats: %#v", encoder)
	}
	if encoder.InstructionsAfter > encoder.InstructionsBefore {
		t.Fatalf("optimizer increased instruction count: %#v", encoder)
	}
	if encoder.SSAValuesBefore == 0 || encoder.SSAValuesAfter >= encoder.SSAValuesBefore {
		t.Fatalf("SSA did not remove redundant values: %#v", encoder)
	}
	if encoder.SpilledRegisters != 0 {
		t.Fatalf("unexpected encoder spills: %#v", encoder)
	}

	decoder, err := DecoderCodegenStats(reflect.TypeOf(TestJITNested{}))
	if err != nil {
		t.Fatal(err)
	}
	if !decoder.DirectDecoder || decoder.DirectPlanNodes == 0 || decoder.DirectFields == 0 {
		t.Fatalf("direct decoder metrics were not reported: %#v", decoder)
	}

	t.Logf("encoder=%+v reduction=%.2f%%", encoder, encoder.InstructionReduction())
	t.Logf("decoder=%+v reduction=%.2f%%", decoder, decoder.InstructionReduction())

	interfaceDecoder, err := DecoderCodegenStats(reflect.TypeOf(codegenInterfaceFixture{}))
	if err != nil {
		t.Fatal(err)
	}
	if !interfaceDecoder.DirectDecoder {
		t.Fatalf("non-empty interface did not use the direct decoder: %#v", interfaceDecoder)
	}
}
