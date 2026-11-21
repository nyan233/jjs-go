package jitloader

import (
	"sync"
)

const pcLnTabMagic uint32 = 0xfffffff1

const (
	nPCDATA      = 4
	nFUNCDATA    = 8
	pcdatInvalid = ^uint32(0)
)

type pcHeader struct {
	magic      uint32
	pad1, pad2 uint8
	minLC      uint8
	ptrSize    uint8
	nfunc      int
	nfiles     uint
	_          uintptr
	funcname   uintptr
	cu         uintptr
	filetab    uintptr
	pctab      uintptr
	pcln       uintptr
}

type functab struct {
	entryOff uint32
	funcOff  uint32
}

type funcInfo struct {
	entryOff  uint32
	nameOff   int32
	args      int32
	deferPC   uint32
	pcsp      uint32
	pcfile    uint32
	pcline    uint32
	npcdata   uint32
	cuOffset  uint32
	startLn   int32
	funcID    uint8
	flag      uint8
	_         [1]byte
	nfuncdata uint8
}

type bitVector struct {
	n    int32
	data *uint8
}

type ptabEntry struct {
	name int32
	typ  int32
}

type textSection struct {
	vaddr    uintptr
	end      uintptr
	baseaddr uintptr
}

type modulehash struct {
	modulename   string
	linktimehash string
	runtimehash  *string
}

type findfuncbucket struct {
	idx        uint32
	subbuckets [16]byte
}

var emptyGCData byte

var retainedModules = struct {
	lock sync.Mutex
	m    map[*moduledata]moduleRetain
}{
	m: make(map[*moduledata]moduleRetain),
}

func putModuleRetain(module *moduledata, retain moduleRetain) {
	retainedModules.lock.Lock()
	retainedModules.m[module] = retain
	retainedModules.lock.Unlock()
}
