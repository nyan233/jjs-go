package compiler

import (
	"reflect"
	"sync"
)

type baseCache[V any] struct {
	mu     sync.RWMutex
	values map[reflect.Type]V
}

func (b *baseCache[V]) init() {
	b.values = make(map[reflect.Type]V, 128)
}

func (b *baseCache[V]) setCompileResult(typ reflect.Type, value V) {
	b.mu.Lock()
	b.values[typ] = value
	b.mu.Unlock()
}

func (b *baseCache[V]) getCompileResult(typ reflect.Type) (V, bool) {
	b.mu.RLock()
	value, ok := b.values[typ]
	b.mu.RUnlock()
	return value, ok
}
