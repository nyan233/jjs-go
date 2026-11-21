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

func TestJITIROptimizesRedundantAndFoldableInstructions(t *testing.T) {
	code := newJITCodeBuilder("test")
	code.mov(_R9, _R9)
	code.movConst(10, _R9)
	code.addConst(5, _R9)
	code.subConst(3, _R9)
	code.movConst(0, _R11)
	code.movConst(1, _R11)
	code.ret()

	instructions, stats, err := code.prepareIR()
	if err != nil {
		t.Fatal(err)
	}
	if len(instructions) != 4 {
		t.Fatalf("instruction count = %d, want 4", len(instructions))
	}
	if stats.RemovedInstructions != 2 || stats.FoldedInstructions != 1 {
		t.Fatalf("unexpected optimization stats: %#v", stats)
	}
	if instructions[1].From.Offset != 2 || instructions[1].To.Reg != _R9.Reg {
		t.Fatalf("folded instruction has unexpected operands: %#v", instructions[1])
	}
}

func TestJITVirtualRegistersReuseNonOverlappingRanges(t *testing.T) {
	code := newJITCodeBuilder("test")
	first := code.newVirtualRegister()
	code.mov(_RAX, first)
	code.mov(first, mem(_RSP, 0))
	second := code.newVirtualRegister()
	code.mov(_RAX, second)
	code.mov(second, mem(_RSP, 8))
	code.ret()

	instructions, stats, err := code.prepareIR()
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
	if encoder.VirtualRegisters == 0 || encoder.RegisterReuse == 0 {
		t.Fatalf("virtual registers were not allocated or reused: %#v", encoder)
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
