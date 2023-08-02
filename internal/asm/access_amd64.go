package asm

func JitGetAX()
func JitGetBX()
func JitGetCX()
func JitGetDX()
func JitGetSP()
func JitGetBP()
func JitGetIP()

func GetR9() uint64

func GetAX() uint64 {
	JitGetAX()
	return GetR9()
}

func GetBX() uint64 {
	JitGetBX()
	return GetR9()
}

func GetCX() uint64 {
	JitGetCX()
	return GetR9()
}

func GetDX() uint64 {
	JitGetDX()
	return GetR9()
}

func GetSP() uint64 {
	JitGetSP()
	return GetR9()
}

func GetBP() uint64 {
	JitGetBP()
	return GetR9()
}

func GetIP() uint64 {
	JitGetIP()
	return GetR9()
}

func ComposeRegGoCtxt() RegGoCtxt

type RegGoCtxt struct {
	RAX uintptr
	RBX uintptr
	RCX uintptr
	RDX uintptr
	R8  uintptr
	R9  uintptr
	R10 uintptr
	R11 uintptr
	R12 uintptr
	R13 uintptr
	R14 uintptr
	R15 uintptr
	RSP uintptr
	RBP uintptr
	RIP uintptr
}
