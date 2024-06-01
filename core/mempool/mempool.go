package mempool

import (
	"github.com/nyan233/jjs-go/internal/rtype"
	"github.com/nyan233/jjs-go/internal/sys"
	"reflect"
	"sync"
	"unsafe"
)

type Stack struct {
	Low  unsafe.Pointer
	High unsafe.Pointer
}

var (
	GlobalArena = NewPool(false, 128*(1<<20))
	StackPool   = sync.Pool{
		New: func() interface{} {
			stack := make([]byte, 4096, 4096)
			return &Stack{
				Low:  unsafe.Pointer(&stack[0]),
				High: unsafe.Pointer(uintptr(unsafe.Pointer(&stack[0])) + 4096),
			}
		},
	}
	nullTypes = [1]unsafe.Pointer{sys.VMalloc(256)}
)

type Pool struct {
	mu sync.Mutex
	// 与[]byte相比不需要处理数组越界, 也不需要编译器插入越界检查指令
	arena rtype.Slice
}

func NewPool(onHeap bool, size uintptr) *Pool {
	if onHeap {
		panic("Pool not support on-heap")
	}
	s := sys.AABytes(size)
	sys.SetExecPermissions(unsafe.Pointer(&s[0]), uintptr(cap(s)))
	return &Pool{arena: rtype.Slice{
		Ptr: unsafe.Pointer(&s[0]),
		Len: 0,
		Cap: cap(s),
	}}
}

func (p *Pool) MallocFixed(size uintptr) uintptr {
	p.mu.Lock()
	defer p.mu.Unlock()
	start := p.arena.Len
	if start+int(size) > p.arena.Cap {
		panic("mempool alloc failed")
	}
	ptr := uintptr(p.arena.Ptr) + uintptr(start)
	p.arena.Len += int(size)
	return ptr
}

func (p *Pool) AllocJitCodeMemory(codeText []byte) uintptr {
	newB := sys.ModifyPermissions2Exec(codeText)
	funcPtr := p.MallocFixed(8)
	*(*uintptr)(unsafe.Pointer(funcPtr)) = (uintptr)(unsafe.Pointer(&newB[0]))
	return funcPtr // ptr size
}

func MallocSlice[T any](len, cap int) []T {
	ptr := GlobalArena.MallocFixed(uintptr(cap) * unsafe.Sizeof(*new(T)))
	return *(*[]T)(unsafe.Pointer(&reflect.SliceHeader{
		Data: ptr,
		Len:  len,
		Cap:  cap,
	}))
}

func MallocAlignSlice[T any](align, cap int) []T {
	aCap := cap + align - (cap % (align))
	ptr := GlobalArena.MallocFixed(uintptr(aCap))
	return *(*[]T)(unsafe.Pointer(&reflect.SliceHeader{
		Data: ptr,
		Len:  cap,
		Cap:  aCap,
	}))
}

func ArenaNew[T any]() *T {
	ptr := GlobalArena.MallocFixed(unsafe.Sizeof(*new(T)))
	return (*T)(unsafe.Pointer(ptr))
}
