package compile

import (
	"encoding/binary"
	"github.com/nyan233/jjs-go/internal/rtype"
	"github.com/twitchyliquid64/golang-asm/obj"
	"github.com/twitchyliquid64/golang-asm/obj/x86"
	"unsafe"
)

/*
	JIT-ABI解释, 对参数和返回值规定了其传递和返回的方式
	参数传递: AX, BX, CX, DX, DI, SI
	参数返回: CX, DX, R9, R10
	栈指针: R8
	Marshaller的abi使用情况
		AX(dest-slice-pointer),BX(data-pointer), CX(状态可刷新寄存器), DX(write-index), R9(reason-code), R10(reason-comment)
*/

const (
	IRStartObject = iota + 127
	IREndObject
	IROutputStaticString
	IROutputDynamicString
	IROutputKeyValSplit
	IROutputNextSplit
	IRSearchKeyMetadata
	IRWriteValue
	IRDeReference
	IRCallMarshaller
	IRCallUnmarshaller
)

type Marshaller func(stackHi uintptr, dest *[]byte, data unsafe.Pointer) (reason int64)
type Unmarshaller func(stackHi uintptr, bytes *[]byte, data unsafe.Pointer) (reason int64)

var (
	_RSP = obj.Addr{
		Type: obj.TYPE_REG,
		Reg:  x86.REG_SP,
	}
	_RAX = obj.Addr{
		Type: obj.TYPE_REG,
		Reg:  x86.REG_AX,
	}
	_RBX = obj.Addr{
		Type: obj.TYPE_REG,
		Reg:  x86.REG_BX,
	}
	_RCX = obj.Addr{
		Type: obj.TYPE_REG,
		Reg:  x86.REG_CX,
	}
	_RDX = obj.Addr{
		Type: obj.TYPE_REG,
		Reg:  x86.REG_DX,
	}
	_R11 = obj.Addr{
		Type: obj.TYPE_REG,
		Reg:  x86.REG_R11,
	}
	_R8 = obj.Addr{
		Type: obj.TYPE_REG,
		Reg:  x86.REG_R8,
	}
	_R9 = obj.Addr{
		Type: obj.TYPE_REG,
		Reg:  x86.REG_R9,
	}
	_R10 = obj.Addr{
		Type: obj.TYPE_REG,
		Reg:  x86.REG_R10,
	}
	_RDI = obj.Addr{
		Type: obj.TYPE_REG,
		Reg:  x86.REG_DI,
	}
	_RSI = obj.Addr{
		Type: obj.TYPE_REG,
		Reg:  x86.REG_SI,
	}
)

var (
	BinaryEncoder binary.ByteOrder
)

func getFunctionCodeAddress(fn interface{}, code bool) uintptr {
	efac := (*rtype.EmptyFace)(unsafe.Pointer(&fn))
	if code {
		return *(*uintptr)(efac.Val)
	}
	return (uintptr)(efac.Val)
}

func init() {
	BinaryEncoder = binary.LittleEndian
}
