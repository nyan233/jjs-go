package compiler

import (
	"maps"
	"sync"
	"sync/atomic"
)

// rcuCache uses read-copy-update with immutable map snapshots. Reads are
// lock-free; writers copy and atomically publish a new map under mu. Go's GC
// reclaims old snapshots after readers release them. Each cold key has its own
// once function so unrelated types can compile concurrently.
type rcuCache[K comparable, V any] struct {
	mu      sync.Mutex
	values  atomic.Pointer[map[K]V]
	pending map[K]func() (V, error)
}

func (c *rcuCache[K, V]) init() {
	values := make(map[K]V)
	c.values.Store(&values)
}

func (c *rcuCache[K, V]) getCompileResult(key K) (value V, ok bool) {
	if values := c.values.Load(); values != nil {
		value, ok = (*values)[key]
	}
	return
}

func (c *rcuCache[K, V]) compile(key K, build func() (V, error)) (V, error) {
	c.mu.Lock()
	if value, ok := c.getCompileResult(key); ok {
		c.mu.Unlock()
		return value, nil
	}
	if c.pending == nil {
		c.pending = make(map[K]func() (V, error))
	}
	get := c.pending[key]
	if get == nil {
		get = sync.OnceValues(func() (V, error) {
			value, err := build()
			if err == nil {
				c.mu.Lock()
				values := make(map[K]V)
				if current := c.values.Load(); current != nil {
					values = maps.Clone(*current)
				}
				values[key] = value
				c.values.Store(&values)
				delete(c.pending, key)
				c.mu.Unlock()
			}
			return value, err
		})
		c.pending[key] = get
	}
	c.mu.Unlock()
	return get()
}
