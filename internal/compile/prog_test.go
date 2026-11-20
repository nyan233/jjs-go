package compile

import (
	"encoding/hex"
	"github.com/twitchyliquid64/golang-asm/obj"
	"github.com/twitchyliquid64/golang-asm/obj/x86"
	"testing"
)

func TestProgram(t *testing.T) {
	t.Run("OrderLink", func(t *testing.T) {
		p := newProgram()
		link := buildCallMemoryCopy(p, p.progRoot)
		link = buildCallOnConst(p, link, "jjs_memmove", nil, nil)
		link = buildBytes2String(p, link, _RDX, _RBX, _RAX, _RCX)
		t.Log(hex.EncodeToString(p.BuildOnFunc(-1, -1)))
	})
	t.Run("InsertLink", func(t *testing.T) {
		p := newProgram()
		link := buildCallMemoryCopy(p, p.progRoot)
		link = buildCallOnConst(p, link, "jjs_memmove", nil, nil)
		link = buildMov(p, link.forward, "quad", _RDX, _RSP)
		link = buildBytes2String(p, link, _RDX, _RBX, _RAX, _RCX)
		t.Log(hex.EncodeToString(p.BuildOnFunc(-1, -1)))
	})
}

func TestLibraryBuild(t *testing.T) {
	t.Run("BuildAvx2Memmove", func(t *testing.T) {
		p := newProgram()
		link, _ := p.AppendP(nil)
		link = buildSimd32Memmove(p, link, 0x2323fff2, 1023, 17)
		t.Log(hex.EncodeToString(p.Build()))
	})
	t.Run("BuildMovabs", func(t *testing.T) {
		p := newProgram()
		link, _ := p.AppendP(nil)
		link = buildQuadMovOnConst(p, link, 0x12345678, _RAX)
		t.Log(hex.EncodeToString(p.Build()))
	})
	t.Run("BuildGrowSlice", func(t *testing.T) {
		p := newProgram()
		link, _ := p.AppendP(nil)
		link = buildGrowSlice(p, link, obj.Addr{
			Type:   obj.TYPE_MEM,
			Reg:    x86.REG_AX,
			Index:  x86.REG_DX,
			Scale:  1,
			Offset: 0x1234,
		}, 0x123232)
		t.Log(hex.EncodeToString(p.Build()))
	})
}
