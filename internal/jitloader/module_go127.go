//go:build go1.27

package jitloader

import "unsafe"

type moduledata struct {
	pcHeader     *pcHeader
	funcnametab  []byte
	cutab        []uint32
	filetab      []byte
	pctab        []byte
	pclntable    []byte
	ftab         []functab
	findfunctab  uintptr
	minpc, maxpc uintptr

	text, etext                uintptr
	noptrdata, enoptrdata      uintptr
	data, edata                uintptr
	bss, ebss                  uintptr
	noptrbss, enoptrbss        uintptr
	covctrs, ecovctrs          uintptr
	end, gcdata, gcbss         uintptr
	types, typedesclen, etypes uintptr
	itaboffset, itabsize       uintptr
	rodata                     uintptr
	gofunc                     uintptr
	epclntab                   uintptr

	textsectmap []textSection

	ptab []ptabEntry

	pluginpath string
	pkghashes  []modulehash

	inittasks []unsafe.Pointer

	modulename   string
	modulehashes []modulehash

	hasmain uint8
	bad     bool

	gcdatamask, gcbssmask bitVector

	typemap map[unsafe.Pointer]unsafe.Pointer

	next *moduledata
}

func setEpclntab(module *moduledata, end uintptr) {
	module.epclntab = end
}
