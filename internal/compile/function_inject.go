package compile

import (
	"github.com/nyan233/jjs-go/internal/asm"
	"github.com/twitchyliquid64/golang-asm/obj"
)

const (
	ABIJIT = iota
	ABI0
	ABIInternal
)

var (
	internalFunctionAddress = make(map[string]funcDesc, 16)
)

type funcDesc struct {
	Name          string
	UnsafeAddress uintptr
	MinUseRegs    []obj.Addr
	SaveRegs      []obj.Addr
	ABI           int
}

func getFuncAddrByName(name string) funcDesc {
	return internalFunctionAddress[name]
}

func setInternalFuncAddr(desc funcDesc) {
	internalFunctionAddress[desc.Name] = desc
}

func init() {
	setInternalFuncAddr(funcDesc{
		Name:          "jjs_jit_memory_copy",
		UnsafeAddress: getFunctionCodeAddress(asm.JitMemoryCopy, true),
		SaveRegs:      []obj.Addr{_RDX, _RAX, _RBX},
		MinUseRegs:    []obj.Addr{_RAX, _RBX, _RCX},
	})
	setInternalFuncAddr(funcDesc{
		Name:          "jjs_jit_save",
		UnsafeAddress: getFunctionCodeAddress(asm.JitSave, true),
		SaveRegs:      nil,
	})
	setInternalFuncAddr(funcDesc{
		Name:          "jjs_jit_recover",
		UnsafeAddress: getFunctionCodeAddress(asm.JitRecover, true),
		SaveRegs:      nil,
	})
	setInternalFuncAddr(funcDesc{
		Name:          "rt_memory_copy",
		UnsafeAddress: getFunctionCodeAddress(asm.RTMemmove, true),
		MinUseRegs:    []obj.Addr{_RDI, _RSI, _RBX},
		SaveRegs:      []obj.Addr{_RDI, _RSI, _RBX, _RAX},
		ABI:           ABIInternal,
	})
}
