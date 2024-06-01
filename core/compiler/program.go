package compiler

import (
	"fmt"
	"github.com/nyan233/jjs-go/core/helper"
	"github.com/nyan233/jjs-go/core/mempool"
	"github.com/twitchyliquid64/golang-asm"
	"github.com/twitchyliquid64/golang-asm/obj"
	"github.com/twitchyliquid64/golang-asm/obj/x86"
	"slices"
	"unsafe"
)

type jumpTrap struct {
	Label string
	Cond  obj.As
}

type Program struct {
	builder  *golangasm.Builder
	instList []*Instruction
	// 程序关联的内存分配
	linkMem    map[int]uintptr
	labels     map[string]*obj.Prog
	jumpMarks  []*obj.Prog
	pc         uint32
	stackUsage uint32
	stackFree  uint32
}

func newProgram() *Program {
	builder, err := golangasm.NewBuilder("amd64", 64)
	if err != nil {
		panic(err)
	}
	return &Program{
		builder:  builder,
		instList: make([]*Instruction, 0, 2048),
		linkMem:  make(map[int]uintptr, 8),
		labels:   make(map[string]*obj.Prog, 8),
	}
}

func (p *Program) Helper() *ProgramHelper {
	return NewProgramHelper(p)
}

func (p *Program) PC() uint32 {
	return uint32(len(p.instList))
}

func (p *Program) PPC() uint32 {
	return p.pc
}

func (p *Program) checkStackUsage() {
	if p.stackUsage-p.stackFree > JIT_MAX_STACK {
		panic(fmt.Sprintf("jit stack life size > %d", JIT_MAX_STACK))
	}
}

func (p *Program) MemAlloc(pc uint32, ptr uintptr) {
	p.linkMem[int(pc)] = ptr
}

func (p *Program) CallGO(addr obj.Addr) {
	p.saveRegs(AllReg...)
	pg := &obj.Prog{
		As: obj.ACALL,
		To: addr,
	}
	p.AddProgram(pg)
	p.recoverRegs(AllReg...)
}

func (p *Program) saveRegs(regs ...obj.Addr) {
	for _, reg := range regs {
		if reg.Type != obj.TYPE_REG {
			panic("save reg type not is REG")
		}
		p.JPush(reg, 0)
	}
}

func (p *Program) recoverRegs(regs ...obj.Addr) {
	slices.Reverse(regs)
	for _, reg := range regs {
		if reg.Type != obj.TYPE_REG {
			panic("recover reg type not is REG")
		}
		p.JPop(reg, 0)
	}
}

/*
lea rsi, [rip+0]
#jpush rsi
jmp   $start
addq  $8, r8
*/
func (p *Program) CallJit(funcPtr *uintptr, inline bool) {
	p.SaveRIP2JitStack(9)
	pg := &obj.Prog{
		As: obj.AJMP,
		To: obj.Addr{
			Type:   obj.TYPE_MEM,
			Offset: int64(uintptr(unsafe.Pointer(funcPtr))),
		},
	}
	p.AddProgram(pg)
	p.JitStackFree(8)
}

func (p *Program) CallSub(start unsafe.Pointer, inline bool) {

}

func (p *Program) AddProgram(objProg *obj.Prog) {
	p.pc++
	p.builder.AddInstruction(objProg)
}

func (p *Program) AddJitInst(inst *Instruction) {
	p.instList = append(p.instList, inst)
}

func (p *Program) JitStackAlloc(size uintptr) {
	p.AddProgram(&obj.Prog{
		As: x86.ASUBQ,
		From: obj.Addr{
			Type:   obj.TYPE_CONST,
			Offset: int64(size),
		},
		To: JitStackReg,
	})
}

func (p *Program) JitStackFree(size uintptr) {
	p.AddProgram(&obj.Prog{
		As: x86.AADDQ,
		From: obj.Addr{
			Type:   obj.TYPE_CONST,
			Offset: int64(size),
		},
		To: JitStackReg,
	})
}

// JPush forward : 中转用到的寄存器指, 有些要Push的值可能是type = TYPE_MEM
func (p *Program) JPush(src obj.Addr, forward int16) {
	p.stackUsage += 8
	p.checkStackUsage()
	pg := &obj.Prog{
		As: x86.ASUBQ,
		From: obj.Addr{
			Type:   obj.TYPE_CONST,
			Offset: 8,
		},
		To: JitStackReg,
	}
	p.AddProgram(pg)
	if src.Type == obj.TYPE_MEM {
		if forward == 0 {
			forward = GoAbiArg1.Reg
		}
		p.AddProgram(&obj.Prog{
			As:   x86.AMOVQ,
			From: src,
			To: obj.Addr{
				Type: obj.TYPE_REG,
				Reg:  forward,
			},
		})
		p.AddProgram(&obj.Prog{
			As: x86.AMOVQ,
			From: obj.Addr{
				Type: obj.TYPE_REG,
				Reg:  forward,
			},
			To: obj.Addr{
				Type:   obj.TYPE_MEM,
				Reg:    JitStackReg.Reg,
				Offset: 0,
			},
		})
	} else {
		pg2 := &obj.Prog{
			As:   x86.AMOVQ,
			From: src,
			To: obj.Addr{
				Type:   obj.TYPE_MEM,
				Reg:    JitStackReg.Reg,
				Offset: 0,
			},
		}
		p.AddProgram(pg2)
	}
}

/*
SaveRIP2JitStack

lea r11, [rip+0] // 48 8d 35 00 00 00 00
#jpush r11
*/
func (p *Program) SaveRIP2JitStack(ipOffset int32) {
	ph := NewProgramHelper(p)
	ph.PadBuf([]byte{0x4c, 0x8d, 0x1d})
	ph.PadBuf(helper.Int2Bytes[int32](ipOffset))
	p.JPush(obj.Addr{
		Type: obj.TYPE_REG,
		Reg:  JitTempReg.Reg,
	}, 0)
}

func (p *Program) CMP(src obj.Addr, dst obj.Addr) {
	p.AddProgram(&obj.Prog{
		As:   x86.ACMPQ,
		From: src,
		To:   dst,
	})
}

// JPop forward : 中转用到的寄存器指, 有些要Push的值可能是type = TYPE_MEM
func (p *Program) JPop(tar obj.Addr, forward int16) {
	if tar.Type == obj.TYPE_CONST {
		panic("target type is TYPE_CONST")
	}
	p.stackFree += 8
	if tar.Type == obj.TYPE_MEM {
		if forward == 0 {
			forward = GoAbiArg1.Reg
		}
		p.AddProgram(&obj.Prog{
			As: x86.AMOVQ,
			From: obj.Addr{
				Type:   obj.TYPE_MEM,
				Reg:    JitStackReg.Reg,
				Offset: 0,
			},
			To: obj.Addr{
				Type: obj.TYPE_REG,
				Reg:  forward,
			},
		})
		p.AddProgram(&obj.Prog{
			As: x86.AMOVQ,
			From: obj.Addr{
				Type: obj.TYPE_REG,
				Reg:  forward,
			},
			To: tar,
		})
	} else {
		pg := &obj.Prog{
			As: x86.AMOVQ,
			From: obj.Addr{
				Type:   obj.TYPE_MEM,
				Reg:    JitStackReg.Reg,
				Offset: 0,
			},
			To: tar,
		}
		p.AddProgram(pg)
	}
	pg2 := &obj.Prog{
		As: x86.AADDQ,
		From: obj.Addr{
			Type:   obj.TYPE_CONST,
			Offset: 8,
		},
		To: JitStackReg,
	}
	p.AddProgram(pg2)
}

func (p *Program) SetLabelStart(label string, pps ...*obj.Prog) {
	_, ok := p.labels[label]
	if ok {
		panic(fmt.Sprintf("dup set label : %s", label))
	}
	if len(pps) <= 0 {
		panic("pps is empty")
	}
	p.labels[label] = pps[0]
	for _, pp := range pps {
		p.AddProgram(pp)
	}
}

func (p *Program) JumpOfCond(as obj.As, label string) {
	pp, ok := p.labels[label]
	// 插入标记, 后续再补齐
	if !ok {
		prog := &obj.Prog{
			As: obj.AJMP,
			To: obj.Addr{
				Type: obj.TYPE_BRANCH,
				Val: &jumpTrap{
					Cond:  as,
					Label: label,
				},
			},
		}
		p.AddProgram(prog)
		p.jumpMarks = append(p.jumpMarks, prog)
	} else {
		p.AddProgram(&obj.Prog{
			As: as,
			To: obj.Addr{
				Type: obj.TYPE_BRANCH,
				Val:  pp,
			},
		})
	}
}

func (p *Program) fixJump() {
	for _, prog := range p.jumpMarks {
		if prog.As != obj.AJMP && prog.To.Type != obj.TYPE_BRANCH {
			panic(fmt.Sprintf("unknown jump mark : %v", prog))
		}
		trap, ok := prog.To.Val.(*jumpTrap)
		if !ok {
			panic(fmt.Sprintf("trap data not is jumpTrap, prog=%v", prog))
		}
		pp, ok := p.labels[trap.Label]
		if !ok {
			panic(fmt.Sprintf("jump target(%s) is not found", trap.Label))
		}
		prog.As = trap.Cond
		prog.To.Val = pp
	}
	p.jumpMarks = nil
}

func (p *Program) Build() ([]byte, error) {
	p.fixJump()
	return p.builder.Assemble(), nil
}

func (p *Program) Build2GoFuncPtr() (*uintptr, error) {
	buf, err := p.Build()
	if err != nil {
		return nil, err
	}
	newBuf := mempool.MallocAlignSlice[byte](16, len(buf))
	copy(newBuf, buf)
	funcPtr := mempool.ArenaNew[uintptr]()
	*funcPtr = uintptr(unsafe.Pointer(&newBuf[0]))
	return funcPtr, nil
}
