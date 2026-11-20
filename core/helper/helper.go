package helper

import "unsafe"

type UINT interface {
	uint8 | uint16 | uint32 | uint64 | uintptr
}

type INT interface {
	int8 | int16 | int32 | int64
}

func Bytes2Int64(b []byte) (val int64) {
	var shift int
	for len(b) != 0 {
		val |= int64(b[0]) << shift
		shift += 8
		b = b[1:]
	}
	return
}

func Int2Bytes[T INT](val T) (b []byte) {
	sizeOf := unsafe.Sizeof(T(0))
	for i := 0; i < int(sizeOf*8); i += 8 {
		b = append(b, byte(val>>i))
	}
	return
}
