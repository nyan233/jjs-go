package compiler

import "reflect"

type runtimeTypeHeader struct {
	Size       uintptr
	PtrBytes   uintptr
	Hash       uint32
	Flags      uint8
	Align      uint8
	FieldAlign uint8
	Kind       uint8
}

const runtimeDirectIface = 1 << 5

// IsDirectInterfaceType reports whether values of typ are stored directly in
// the data word of an empty interface instead of behind a pointer to a box.
func IsDirectInterfaceType(typ reflect.Type) bool {
	if typ == nil {
		return false
	}
	header := (*runtimeTypeHeader)(runtimeTypePointer(typ))
	return runtimeDirectInterface(header)
}
