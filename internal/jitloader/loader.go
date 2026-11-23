package jitloader

import (
	"fmt"
	"sync"
	"unsafe"
)

// Runtime module-chain registration is not reentrant on all supported Go
// versions. Building the module and its executable mapping can run in parallel.
var moduleRegisterMu sync.Mutex

// Function is a pointer to a loaded function value.
type Function unsafe.Pointer

// Options controls runtime-visible metadata for generated functions.
type Options struct {
	NoPreempt bool
}

// Loader owns the names and options used to register generated functions.
type Loader struct {
	Name string
	File string
	Options
}

// Pcvalue maps a program-counter range to Val.
type Pcvalue struct {
	PC  uint32
	Val int32
}

// Pcdata is a PC-to-value table.
type Pcdata []Pcvalue

// LoadOne loads one generated function into the current Go module chain.
func (loader Loader) LoadOne(
	text []byte,
	funcName string,
	_ int,
	argSize int,
	argPtrs []bool,
	localPtrs []bool,
	pcsp Pcdata,
) Function {
	module := buildModule(moduleSpec{
		loader:    loader,
		text:      text,
		name:      funcName,
		argSize:   argSize,
		argPtrs:   argPtrs,
		localPtrs: localPtrs,
		pcsp:      pcsp,
	})
	moduleRegisterMu.Lock()
	registerModule(module.metadata)
	moduleRegisterMu.Unlock()
	address := module.metadata.text
	return Function(&address)
}

type moduleSpec struct {
	loader    Loader
	text      []byte
	name      string
	argSize   int
	argPtrs   []bool
	localPtrs []bool
	pcsp      Pcdata
}

type loadedModule struct {
	metadata *moduledata
	retain   moduleRetain
}

type moduleRetain struct {
	text        []byte
	funcNames   []byte
	cuTable     []uint32
	fileTable   []byte
	pcTable     []byte
	pcLineTable []byte
	funcTable   []functab
	findFunc    []byte
	funcData    []byte
}

func loadedFunctionName(spec moduleSpec) string {
	return fmt.Sprintf("%s%s", spec.loader.Name, spec.name)
}
