package test

import (
	"encoding/hex"
	asm "github.com/twitchyliquid64/golang-asm"
	"github.com/twitchyliquid64/golang-asm/obj"
	"github.com/twitchyliquid64/golang-asm/obj/x86"
	"testing"
)

func TestAssembler(t *testing.T) {
	b, _ := asm.NewBuilder("amd64", 64)
	p := b.NewProg()
	p.As = x86.AMOVQ
	p.From = obj.Addr{
		Type:   obj.TYPE_CONST,
		Offset: 0x128,
	}
	p.To = obj.Addr{
		Type:  obj.TYPE_MEM,
		Reg:   x86.REG_AX,
		Index: x86.REG_DX,
		Scale: 1,
	}
	b.AddInstruction(p)
	t.Log(hex.EncodeToString(b.Assemble()))
}
