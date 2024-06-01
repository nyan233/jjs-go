package test

import (
	"github.com/nyan233/jjs-go/internal/rtype"
	"testing"
	"unsafe"
)

func print1() {
	println("Hello-World")
}

func TestAsm(t *testing.T) {
	var t1 = any(print1)
	var t2 = print1
	ef := (*rtype.EmptyFace)(unsafe.Pointer(&t1))
	funcPtr := *(*uintptr)(unsafe.Pointer(&t2))
	t.Log(uintptr(ef.Val))
	t.Log(funcPtr)
	t.Log(*(*uintptr)(ef.Val))
	var t3 = GetJitGenCallTest()
	var t4 = &t3
	var jitCallTest = *(*func(fn uintptr))(unsafe.Pointer(&t4))
	//CallTest(*(*uintptr)(ef.Val))
	jitCallTest(uintptr(ef.Val))
}

func TestBaseFunc(t *testing.T) {
	t.Run("GetPC", func(t *testing.T) {
		t.Log(GetPC())
	})
}
