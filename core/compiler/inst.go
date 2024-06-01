package compiler

import (
	"sync"
	"unsafe"
)

type InstructionDef uint32

type Instruction struct {
	Def    InstructionDef
	Arg    uint64
	Detail interface{}
}

var InstSetPool = sync.Pool{
	New: func() any {
		val := make(InstSet, 0, 4096)
		return &val
	},
}

type UINT interface {
	uint | uint8 | uint16 | uint32 | uint64 | uintptr
}

func ParseUList[T UINT](arg uint64) (val []T) {
	size := unsafe.Sizeof(T(0))
	switch size {
	case 8:
		val = []T{T(arg)}
	case 4:
		val = []T{T(arg << 32), T(arg)}
	case 2:
		val = make([]T, 0, 64/size)
		val[0] = T(arg << 48)
		val[1] = T(arg << 32)
		val[2] = T(arg << 16)
		val[3] = T(arg << 0)
	case 1:
		val = make([]T, 0, 64/size)
		val[0] = T(arg << 56)
		val[1] = T(arg << 48)
		val[2] = T(arg << 40)
		val[3] = T(arg << 32)
		val[4] = T(arg << 24)
		val[5] = T(arg << 16)
		val[6] = T(arg << 8)
		val[7] = T(arg)
	}
	return
}

type InstSet []*Instruction

func (i *InstSet) Len() int {
	return len(*i)
}

func (i *InstSet) Reset() {
	for index := range *i {
		(*i)[index] = nil
	}
	*i = (*i)[:0]
	return
}

func (i *InstSet) Append(v *Instruction) {
	*i = append(*i, v)
}

const (
	InsDeRef InstructionDef = iota + 314
	InsWString
	InsWNumber
	InsSlice2Array
	InsArrayEncStart
	InsArrayWElem
	InsArrayNext
	InsArrayEncEnd
	InsStructEncStart
	InsStructNextField
	InsStructWField
	InsStructEncEnd
	InsMapEncStart
	InsMapEncRange
	InsMapEncRangeEnd
	InsMapWKey
	InsMapNextKey
	InsMapEncEnd
	InsWAny
	InsWBool
)
