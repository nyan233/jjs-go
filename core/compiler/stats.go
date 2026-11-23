package compiler

import "reflect"

// CodegenStats reports the effect of JIT SSA optimization and register
// allocation. It is intended for diagnostics and benchmark reports.
type CodegenStats struct {
	Functions           int
	SSAValuesBefore     int
	SSAValuesAfter      int
	InstructionsBefore  int
	InstructionsAfter   int
	RemovedInstructions int
	FoldedInstructions  int
	BranchesRemoved     int
	VirtualRegisters    int
	RegisterReuse       int
	SpilledRegisters    int
	CodeBytes           int
	DirectDecoder       bool
	DirectPlanNodes     int
	DirectFields        int
}

func (s CodegenStats) add(other jitCodegenStats) CodegenStats {
	s.SSAValuesBefore += other.SSAValuesBefore
	s.SSAValuesAfter += other.SSAValuesAfter
	s.InstructionsBefore += other.InstructionsBefore
	s.InstructionsAfter += other.InstructionsAfter
	s.RemovedInstructions += other.RemovedInstructions
	s.FoldedInstructions += other.FoldedInstructions
	s.BranchesRemoved += other.BranchesRemoved
	s.VirtualRegisters += other.VirtualRegisters
	s.RegisterReuse += other.RegisterReuse
	s.SpilledRegisters += other.SpilledRegisters
	s.CodeBytes += other.CodeBytes
	return s
}

// InstructionReduction returns the percentage of backend instructions removed by
// optimization. A negative result means lowering increased the instruction count.
func (s CodegenStats) InstructionReduction() float64 {
	if s.InstructionsBefore == 0 {
		return 0
	}
	return float64(s.InstructionsBefore-s.InstructionsAfter) * 100 / float64(s.InstructionsBefore)
}

// EncoderCodegenStats compiles typ and returns its encoder code-generation
// metrics without installing a new public encoding plan.
func EncoderCodegenStats(typ reflect.Type) (CodegenStats, error) {
	program, err := compileJITEncoder(typ)
	if err != nil {
		return CodegenStats{}, err
	}
	return program.stats, nil
}

// DecoderCodegenStats compiles typ and returns its decoder code-generation
// metrics without installing a new public decoding plan.
func DecoderCodegenStats(typ reflect.Type) (CodegenStats, error) {
	program, err := compileJITDecoder(typ)
	if err != nil {
		return CodegenStats{}, err
	}
	return program.stats, nil
}
