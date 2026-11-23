package compiler

import (
	"maps"
	"sync"
	"sync/atomic"
)

// Published maps are immutable. Readers never contend on a shared lock, and
// each cold key has its own once function so unrelated types can compile.
type baseCache[K comparable, V any] struct {
	mu      sync.Mutex
	values  atomic.Pointer[map[K]V]
	pending map[K]func() (V, error)
}

func (b *baseCache[K, V]) init() {
	values := make(map[K]V)
	b.values.Store(&values)
}

func (b *baseCache[K, V]) getCompileResult(key K) (value V, ok bool) {
	if values := b.values.Load(); values != nil {
		value, ok = (*values)[key]
	}
	return
}

func (b *baseCache[K, V]) compile(key K, build func() (V, error)) (V, error) {
	b.mu.Lock()
	if value, ok := b.getCompileResult(key); ok {
		b.mu.Unlock()
		return value, nil
	}
	if b.pending == nil {
		b.pending = make(map[K]func() (V, error))
	}
	get := b.pending[key]
	if get == nil {
		get = sync.OnceValues(func() (V, error) {
			value, err := build()
			if err == nil {
				b.mu.Lock()
				values := make(map[K]V)
				if current := b.values.Load(); current != nil {
					values = maps.Clone(*current)
				}
				values[key] = value
				b.values.Store(&values)
				delete(b.pending, key)
				b.mu.Unlock()
			}
			return value, err
		})
		b.pending[key] = get
	}
	b.mu.Unlock()
	return get()
}
