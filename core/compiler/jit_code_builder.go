package compiler

import (
	"github.com/nyan233/jjs-go/internal/jitloader"
	"github.com/twitchyliquid64/golang-asm/obj"
)

type jitCodegenStats struct {
	InstructionsBefore  int
	InstructionsAfter   int
	RemovedInstructions int
	FoldedInstructions  int
	BranchesRemoved     int
	VirtualRegisters    int
	RegisterReuse       int
	SpilledRegisters    int
	CodeBytes           int
	SSABlocks           int
	SSAValues           int
	SSAPhis             int
}

type jitCodeBuilder struct {
	name         string
	instructions []*obj.Prog
	virtualCount int
	stats        jitCodegenStats
	codeWords    []uint32
	dataOffset   int64
	pcsp         jitloader.Pcdata
	encoderFrame bool
	frameSize    int64
	labels       []*jitLabel
	opaqueRanges []jitCodeRange
	knownCalls   []jitKnownCall
}

// Opaque kernels keep their internal control flow and instruction scheduling.
type jitCodeRange struct{ start, end int }
type jitKnownCall struct {
	pc    int
	reads uint64
}

type jitLabel struct {
	prog     *obj.Prog
	position int
	branches []int
}

func (c *jitCodeBuilder) newLabel() *jitLabel {
	l := &jitLabel{prog: &obj.Prog{As: obj.ANOP}, position: -1}
	c.labels = append(c.labels, l)
	return l
}

const (
	jitEqual = iota
	jitNotEqual
	jitBelow
	jitAboveEqual
	jitNegative
	jitNonNegative
	jitAlways
)

const jitInlineDepth = 4

// Encoder locals, from low to high: 32 bytes of numeric scratch, four
// counters, four saved roots, current root and context. Pointer slots are
// initialized before the first call and described to the runtime's stack mover.
const jitEncoderFrameExtra = 112

func (c *jitCodeBuilder) contextSlot() int64        { return c.frameSize - 16 }
func (c *jitCodeBuilder) dataSlot() int64           { return c.frameSize - 24 }
func (c *jitCodeBuilder) rootSlot(depth int) int64  { return c.frameSize - 32 - int64(depth)*8 }
func (c *jitCodeBuilder) countSlot(depth int) int64 { return c.frameSize - 64 - int64(depth)*8 }
func (c *jitCodeBuilder) scratchSlot() int64        { return c.frameSize - 120 }

func (c *jitCodeBuilder) localPointers() []bool {
	if !c.encoderFrame {
		return jitLocalPointers()
	}
	return []bool{false, false, false, false, false, false, false, false, true, true, true, true, true, true}
}

func (c *jitCodeBuilder) initEncoderLocals() {
	if !c.encoderFrame {
		return
	}
	c.mov(_R12, mem(_RSP, c.dataSlot()))
	c.movConst(0, _R11)
	for i := 0; i < jitInlineDepth; i++ {
		c.mov(_R11, mem(_RSP, c.rootSlot(i)))
	}
	// Word copies of the final digits may include up to seven padding bytes.
	// Never expose uninitialized stack contents through output capacity.
	c.mov(_R11, mem(_RSP, c.scratchSlot()+24))
}

func newJITCodeBuilder(name string) *jitCodeBuilder {
	return &jitCodeBuilder{name: name}
}

func (c *jitCodeBuilder) emit(prog *obj.Prog) {
	c.instructions = append(c.instructions, prog)
}

func mem(base obj.Addr, offset int64) obj.Addr {
	return obj.Addr{Type: obj.TYPE_MEM, Reg: base.Reg, Offset: base.Offset + offset}
}

func registerAddr(register int) obj.Addr {
	return obj.Addr{Type: obj.TYPE_REG, Reg: int16(register)}
}
