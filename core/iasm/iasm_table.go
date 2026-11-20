package iasm

import (
	"fmt"
	"github.com/twitchyliquid64/golang-asm/obj"
	"unsafe"
)

var _Table = map[string]*uintptr{}

func _RegisterFunc(name string, fn interface{}) {
	ef := (*[2]unsafe.Pointer)(unsafe.Pointer(&fn))
	funcPtr := (*uintptr)(ef[1])
	_Table[name] = funcPtr
}

func GetJitFunc2Addr(name string) obj.Addr {
	funcPtr, ok := _Table[name]
	if !ok {
		panic(fmt.Sprintf("func(%s) is not found", name))
	}
	return obj.Addr{
		Type:   obj.TYPE_MEM,
		Offset: int64(uintptr(unsafe.Pointer(funcPtr))),
	}
}

func init() {
	_RegisterFunc("memmove", memmove)
	_RegisterFunc("growslice", growslice)
}
