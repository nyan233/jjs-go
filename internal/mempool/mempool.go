package mempool

import (
	"github.com/nyan233/jjs-go/internal/rtype"
	"github.com/nyan233/jjs-go/internal/sys"
	"sync"
	"unsafe"
)

type Stack struct {
	Low  unsafe.Pointer
	High unsafe.Pointer
}

var (
	GlobalArena = NewPool(false, 4096*1024*1024)
	StackPool   = sync.Pool{
		New: func() interface{} {
			stack := make([]byte, 4096, 4096)
			return &Stack{
				Low:  unsafe.Pointer(&stack[0]),
				High: unsafe.Pointer(uintptr(unsafe.Pointer(&stack[0])) + 4096),
			}
		},
	}
)

type Pool struct {
	mu sync.Mutex
	// 与[]byte相比不需要处理数组越界, 也不需要编译器插入越界检查指令
	arena rtype.Slice
	size  uintptr
}

func NewPool(onHeap bool, size uintptr) *Pool {
	if onHeap {
		panic("Pool not support on-heap")
	}
	return &Pool{size: size}
}

func (p *Pool) MallocFixed(size uintptr) unsafe.Pointer {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.ensureLocked()
	start := p.arena.Len
	if size > uintptr(p.arena.Cap-start) {
		panic("mempool alloc failed")
	}
	ptr := unsafe.Add(p.arena.Ptr, start)
	p.arena.Len += int(size)
	return ptr
}

func (p *Pool) ensureLocked() {
	if p.arena.Ptr != nil {
		return
	}
	if p.size == 0 {
		panic("mempool allocation size is zero")
	}
	p.arena = rtype.Slice{
		Ptr: sys.VMalloc(p.size),
		Len: 0,
		Cap: int(p.size),
	}
}

func (p *Pool) AllocJitCodeMemory(codeText []byte) unsafe.Pointer {
	newB := sys.ModifyPermissions2Exec(codeText)
	funcPtr := p.MallocFixed(8)
	*(*uintptr)(funcPtr) = uintptr(unsafe.Pointer(&newB[0]))
	return funcPtr // ptr size
}

func MallocSlice[T any](len, cap int) []T {
	ptr := GlobalArena.MallocFixed(uintptr(cap) * unsafe.Sizeof(*new(T)))
	return unsafe.Slice((*T)(ptr), cap)[:len]
}

func ArenaNew[T any]() *T {
	ptr := GlobalArena.MallocFixed(unsafe.Sizeof(*new(T)))
	return (*T)(ptr)
}
