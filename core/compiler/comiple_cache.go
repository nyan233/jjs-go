package compiler

import (
	"github.com/nyan233/jjs-go/core/container"
	"reflect"
)

type baseCache[V any] struct {
	rcuMap *container.RCUMap[reflect.Type, V]
}

func (b *baseCache[V]) init() {
	b.rcuMap = container.NewRCUMap[reflect.Type, V](128)
}

func (b *baseCache[V]) setCompileResult(typ reflect.Type, value V) {
	b.rcuMap.Store(typ, value)
}

func (b *baseCache[V]) getCompileResult(typ reflect.Type) (V, bool) {
	return b.rcuMap.LoadOk(typ)
}
