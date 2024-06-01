package compiler

import (
	"github.com/nyan233/jjs-go/core/container"
	"reflect"
	"unsafe"
)

type baseCache struct {
	rcuMap *container.RCUMap[reflect.Type, unsafe.Pointer]
}

func (b *baseCache) init() {
	b.rcuMap = container.NewRCUMap[reflect.Type, unsafe.Pointer](128)
}

func (b *baseCache) setCompileResult(typ reflect.Type, function unsafe.Pointer) {
	b.rcuMap.Store(typ, function)
}

func (b *baseCache) getCompileResult(typ reflect.Type) (unsafe.Pointer, bool) {
	return b.rcuMap.LoadOk(typ)
}
