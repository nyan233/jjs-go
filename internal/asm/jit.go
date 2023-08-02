package asm

import _ "unsafe"

const (
	JIT_CODE_DEFAULT_STACK_SIZE = 4096
	JIT_CODE_DEFAULT_TEXT_SIZE  = 4096
)

type jitCode struct {
	// of-heap memory, 4096/16384Byte
	stack stack
	code  uintptr
}

func (j *jitCode) UseStack(fn func(), lock bool) {
	if lock {
		jitstack(j.stack, fn, 1)
	}
	jitstack(j.stack, fn, 0)
}

func getStack() uintptr {
	return 0
}

func jitGetStack()

func jitCall()

func goCodeJitCall(prog *jitCode)

//go:linkname systemstack runtime.systemstack
func systemstack(fn func())

func jitstack(ss stack, fn func(), lockflag int)

func jitstack_switch()

//go:linkname runtime_proc_pin runtime.procPin
func runtime_proc_pin() int

//go:linkname runtime_proc_unpin runtime.procUnpin
func runtime_proc_unpin()

func procPin() {
	runtime_proc_pin()
}

func procUnpin() {
	runtime_proc_unpin()
}
