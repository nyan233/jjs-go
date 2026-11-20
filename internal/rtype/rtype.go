package rtype

import "unsafe"

type Slice struct {
	Ptr unsafe.Pointer
	Len int
	Cap int
}

// NPSlice NO Pointer
type NPSlice struct {
	Ptr uintptr
	Len int
	Cap int
}

type EmptyFace struct {
	Type unsafe.Pointer
	Val  unsafe.Pointer
}

type ItabFace struct {
	Itab unsafe.Pointer
	Val  unsafe.Pointer
}
