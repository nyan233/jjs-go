package compile

import (
	asm "github.com/twitchyliquid64/golang-asm"
	"github.com/twitchyliquid64/golang-asm/obj"
	"github.com/twitchyliquid64/golang-asm/obj/x86"
)

type objLink struct {
	Prog    *obj.Prog
	next    *objLink
	forward *objLink
}

type program struct {
	_builder       *asm.Builder
	newBuilderFunc func() *asm.Builder
	progRoot       *objLink
	pcOffset       int
	sp             uint64
	bp             uint64
	fs             uint64
	gs             uint64
}

func newProgram() *program {
	builder, err := asm.NewBuilder("amd64", 64)
	if err != nil {
		panic(err)
	}
	return &program{
		_builder: builder,
		newBuilderFunc: func() *asm.Builder {
			builder, err := asm.NewBuilder("amd64", 64)
			if err != nil {
				panic(err)
			}
			return builder
		},
	}
}

func (p *program) RequireSP(objl *objLink, size uint64) {
	ol2, _ := p.AppendP(objl)
	ol2.Prog.As = x86.ASUBQ
	ol2.Prog.From = obj.Addr{
		Type:   obj.TYPE_CONST,
		Offset: int64(size),
	}
	ol2.Prog.To = _RSP
	p.sp += size
}

func (p *program) AppendP(objl *objLink) (l *objLink, pcOffset int) {
	if objl == nil && p.progRoot == nil {
		p.progRoot = &objLink{
			Prog: p._builder.NewProg(),
		}
		return p.progRoot, p.pcOffset
	}
	if objl == nil {
		return nil, -1
	}
	if objl.next == nil {
		objl.next = &objLink{
			Prog:    p._builder.NewProg(),
			forward: objl,
		}
		p.pcOffset++
		return objl.next, p.pcOffset
	}
	newOl := &objLink{
		Prog:    p._builder.NewProg(),
		forward: objl,
		next:    objl.next,
	}
	objl.next = newOl
	p.pcOffset++
	return newOl, p.pcOffset
}

func (p *program) Build() []byte {
	nb, _ := p.buildNewContainer(-1, -1, false)
	return nb.Assemble()
}

// BuildOnFunc ABI == 1 (Build Marshaller ABI)
//
//	ABI == 2 (Build Unmarshaller ABI)
//	ABI <= 0 (No Build)
func (p *program) BuildOnFunc(abi int, baseLen int) []byte {
	nb, last := p.buildNewContainer(abi, baseLen, true)
	p.checkAndInsertRet(nb, last.Prog)
	return nb.Assemble()
}

func (p *program) buildNewContainer(abi int, baseLen int, rewrite bool) (nb *asm.Builder, last *objLink) {
	nb = p.newBuilderFunc()
	root := p.progRoot
	if rewrite {
		defer p.rewriteJitFuncCallABI(nb, abi)(nb, baseLen)
	}
	for {
		// 最后一个为无效指令时不插入
		if root.next == nil && root.Prog.As == obj.AXXX {
			break
		}
		nb.AddInstruction(root.Prog)
		if root.next == nil {
			break
		}
		root = root.next
	}
	return nb, root
}

func (p *program) rewriteJitFuncCallABI(b *asm.Builder, abi int) func(b2 *asm.Builder, baseLen int) {
	// rewrite base ABIInternal
	b.AddInstruction(&obj.Prog{
		As:   x86.AMOVQ,
		From: _RAX,
		To:   _R8,
	})
	// raw bytes ptr
	b.AddInstruction(&obj.Prog{
		As:   x86.AMOVQ,
		From: _RBX,
		To:   _R9,
	})
	b.AddInstruction(&obj.Prog{
		As:   x86.AXORQ,
		From: _RDX,
		To:   _RDX,
	})
	if abi == 1 {
		// mov rsi, bytes.cap
		b.AddInstruction(&obj.Prog{
			As: x86.AMOVQ,
			From: obj.Addr{
				Type:   obj.TYPE_MEM,
				Reg:    x86.REG_BX,
				Offset: 16,
			},
			To: obj.Addr{
				Type: obj.TYPE_REG,
				Reg:  x86.REG_SI,
			},
		})
		// mov rax, QWORD PTR [rax]
		b.AddInstruction(&obj.Prog{
			As: x86.AMOVQ,
			From: obj.Addr{
				Type: obj.TYPE_MEM,
				Reg:  x86.REG_BX,
			},
			To: _RAX,
		})
		b.AddInstruction(&obj.Prog{
			As:   x86.AMOVQ,
			From: _RCX,
			To:   _RBX,
		})
	}
	// write-background
	return func(b2 *asm.Builder, baseLen int) {
		b2.AddInstruction(&obj.Prog{
			As:   x86.AMOVQ,
			From: _R9,
			To: obj.Addr{
				Type: obj.TYPE_REG,
				Reg:  x86.REG_DI,
			},
		})
		switch abi {
		case 1:
			if baseLen > 0 {
				b2.AddInstruction(&obj.Prog{
					As:   x86.AADDQ,
					From: obj.Addr{Type: obj.TYPE_CONST, Offset: int64(baseLen)},
					To:   _RDX,
				})
			}
			// write bytes len
			b2.AddInstruction(&obj.Prog{
				As:   x86.AMOVQ,
				From: _RDX,
				To: obj.Addr{
					Type:   obj.TYPE_MEM,
					Reg:    x86.REG_R9,
					Offset: 8,
				},
			})
		}
	}
}

func (p *program) callFunc(b *asm.Builder, name string) {
	link := buildACALL(nil, nil, getFuncAddrByName(name))
	for link.forward != nil {
		link = link.forward
	}
	for link.next != nil {
		b.AddInstruction(link.Prog)
		link = link.next
	}
}

func (p *program) checkAndInsertRet(b *asm.Builder, last *obj.Prog) {
	if last.As != obj.ARET {
		np := b.NewProg()
		np.As = obj.ARET
		b.AddInstruction(np)
	}
}
