package asm

import (
	"unsafe"
)

const (
	_RUNTIME_PROC_MAX = 1<<8 - 1
)

type stack struct {
	lo uintptr
	hi uintptr
}

type gsignalStack struct {
	stack       stack
	stackguard0 uintptr
	stackguard1 uintptr
	stktopsp    uintptr
}

// link runtime/m
type m struct {
	g0        *g
	morebuf   gobuf
	divmod    uint32
	_         uint32
	procId    uint64
	gsignal   *g
	gSigStack gsignalStack
	sigmask   [2]uint32
	tls       [6]uintptr
	mstartfn  func()
	curg      *g
}

type g struct {
	stack
	stackguard0 uintptr        // offset known to liblink
	stackguard1 uintptr        // offset known to liblink
	_panic      unsafe.Pointer // innermost panic - offset known to liblink
	_defer      unsafe.Pointer // innermost defer
	m           *m
	sched       gobuf
}

type gobuf struct {
	// The offsets of sp, pc, and g are known to (hard-coded in) libmach.
	//
	// ctxt is unusual with respect to GC: it may be a
	// heap-allocated funcval, so GC needs to track it, but it
	// needs to be set and cleared from assembly, where it's
	// difficult to have write barriers. However, ctxt is really a
	// saved, live register, and we only ever exchange it between
	// the real register and the gobuf. Hence, we treat it as a
	// root during stack scanning, which means assembly that saves
	// and restores it doesn't need write barriers. It's still
	// typed as a pointer so that any other writes from Go get
	// write barriers.
	sp   uintptr
	pc   uintptr
	g    guintptr
	ctxt unsafe.Pointer
	ret  uintptr
	lr   uintptr
	bp   uintptr // for framepointer-enabled architectures
}

type puintptr uintptr
type guintptr uintptr

func jitGetg()

func getg() *g

func currentg() *g {
	m := getg().m
	return m.curg
}
