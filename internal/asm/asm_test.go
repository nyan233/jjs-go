package asm

import (
	"testing"
	"unsafe"
)

func TestRegisterAccess(t *testing.T) {
	var funcSet = map[string]func() uint64{
		"AX": GetAX,
		"BX": GetBX,
		"CX": GetCX,
		"DX": GetDX,
		"SP": GetSP,
		"BP": GetBP,
		"IP": GetIP,
	}
	for k, v := range funcSet {
		t.Log(k, " -> ", v())
	}
	ctxt := ComposeRegGoCtxt()
	t.Log("RAX: ", ctxt.RAX)
	t.Log("RBX: ", ctxt.RBX)
	t.Log("RCX: ", ctxt.RCX)
	t.Log("R8: ", ctxt.R8)
	t.Log("R9: ", ctxt.R9)
	t.Log("R10: ", ctxt.R10)
	t.Log("R11: ", ctxt.R11)
	t.Log("R12: ", ctxt.R12)
	t.Log("R13: ", ctxt.R13)
	t.Log("R14: ", ctxt.R14)
	t.Log("R15: ", ctxt.R15)
	t.Log("RSP: ", ctxt.RSP)
	t.Log("RBP: ", ctxt.RBP)
	t.Log("RIP: ", ctxt.RIP)
}

func TestRuntimeFunc(t *testing.T) {
	var buf1 [2049]byte
	var buf2 [2049]byte
	RTMemmove(unsafe.Pointer(&buf1), unsafe.Pointer(&buf2), 2049)
}

func TestRTGrowSlice(t *testing.T) {
	a := RTGrowSlice(nil, 0, 0, 0, nil)
	_ = a.Ptr
	_ = a.Len
	_ = a.Cap
}
