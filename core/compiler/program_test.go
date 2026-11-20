package compiler

import (
	"encoding/hex"
	"github.com/twitchyliquid64/golang-asm/obj"
	"github.com/twitchyliquid64/golang-asm/obj/x86"
	"testing"
	"unsafe"
)

func TestConJump(t *testing.T) {
	t.Run("BeforeSetTarget", func(t *testing.T) {
		p := newProgram()
		p.AddProgram(&obj.Prog{
			As: x86.AMOVQ,
			From: obj.Addr{
				Type:   obj.TYPE_CONST,
				Offset: 23,
			},
			To: GoAbiArg1,
		})
		p.AddProgram(&obj.Prog{
			As: x86.AMOVQ,
			From: obj.Addr{
				Type:   obj.TYPE_CONST,
				Offset: 22,
			},
			To: GoAbiArg2,
		})
		p.SetLabelStart("start", &obj.Prog{
			As:   x86.AMOVQ,
			From: GoAbiArg3,
			To:   GoAbiArg4,
		})
		p.JumpOfCond(x86.AJGT, "start")
		p.AddProgram(&obj.Prog{
			As: obj.ARET,
		})
		buf, err := p.Build()
		if err != nil {
			t.Fatal(err)
		}
		t.Log(hex.EncodeToString(buf))
		funcPtr, err := p.Build2GoFuncPtr()
		if err != nil {
			t.Fatal(err)
		}
		fn := *(*func())(unsafe.Pointer(&funcPtr))
		fn()
	})
	t.Run("AfterSetTarget", func(t *testing.T) {
		p := newProgram()
		p.AddProgram(&obj.Prog{
			As: x86.AMOVQ,
			From: obj.Addr{
				Type:   obj.TYPE_CONST,
				Offset: 23,
			},
			To: GoAbiArg1,
		})
		p.AddProgram(&obj.Prog{
			As: x86.AMOVQ,
			From: obj.Addr{
				Type:   obj.TYPE_CONST,
				Offset: 22,
			},
			To: GoAbiArg2,
		})
		p.JumpOfCond(x86.AJGT, "start")
		p.SetLabelStart("start", &obj.Prog{
			As: obj.ARET,
		})
		buf, err := p.Build()
		if err != nil {
			t.Fatal(err)
		}
		t.Log(hex.EncodeToString(buf))
		funcPtr, err := p.Build2GoFuncPtr()
		if err != nil {
			t.Fatal(err)
		}
		fn := *(*func())(unsafe.Pointer(&funcPtr))
		fn()
	})
}

func TestPad(t *testing.T) {
	p := newProgram()
	p.Helper().PadBuf([]byte{0x48, 0x89, 0xd8})
	codeBuf, err := p.Build()
	if err != nil {
		t.Fatal(err)
	}
	t.Log(hex.EncodeToString(codeBuf))
	p.Helper().PadWord(0x8948)
	p.Helper().PadByte(0xd8)
	codeBuf, err = p.Build()
	if err != nil {
		t.Fatal(err)
	}
	t.Log(hex.EncodeToString(codeBuf))
}
