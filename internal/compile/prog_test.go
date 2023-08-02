package compile

import (
	"encoding/hex"
	"testing"
)

func TestProgram(t *testing.T) {
	t.Run("OrderLink", func(t *testing.T) {
		p := newProgram()
		link := buildCallMemoryCopy(p, p.progRoot)
		link = buildCallOnConst(p, link, "jjs_memmove", nil, nil)
		link = buildBytes2String(p, link, _RDX, _RBX, _RAX, _RCX)
		t.Log(hex.EncodeToString(p.BuildOnFunc(-1)))
	})
	t.Run("InsertLink", func(t *testing.T) {
		p := newProgram()
		link := buildCallMemoryCopy(p, p.progRoot)
		link = buildCallOnConst(p, link, "jjs_memmove", nil, nil)
		link = buildMov(p, link.forward, "quad", _RDX, _RSP)
		link = buildBytes2String(p, link, _RDX, _RBX, _RAX, _RCX)
		t.Log(hex.EncodeToString(p.BuildOnFunc(-1)))
	})
}
