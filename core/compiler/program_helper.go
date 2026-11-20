package compiler

import (
	"github.com/twitchyliquid64/golang-asm/obj"
	"github.com/twitchyliquid64/golang-asm/obj/x86"
)

type ProgramHelper struct {
	p *Program
}

func NewProgramHelper(p *Program) *ProgramHelper {
	return &ProgramHelper{
		p: p,
	}
}

func (ph *ProgramHelper) PadByte(b byte) {
	ph.Pad(x86.ABYTE, int64(b))
}

func (ph *ProgramHelper) PadWord(w uint16) {
	ph.Pad(x86.AWORD, int64(w))
}

func (ph *ProgramHelper) PadBuf(buf []byte) {
	for _, val := range buf {
		ph.PadByte(val)
	}
}

func (ph *ProgramHelper) Pad(as obj.As, val int64) {
	ph.p.AddProgram(&obj.Prog{
		As: as,
		From: obj.Addr{
			Type:   obj.TYPE_CONST,
			Offset: val,
		},
	})
}
