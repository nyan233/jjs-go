package compiler

import (
	"fmt"
	"github.com/bytedance/sonic/loader"
	"sync"
	"sync/atomic"
	"unsafe"
)

var jitFunctionID atomic.Uint64
var jitTypeRegistry sync.Map

func loadJITFunction(code []byte, prefix string) loader.Function {
	id := jitFunctionID.Add(1)
	instance := jitLoader
	instance.Name = fmt.Sprintf("jjs.jit.%d.", id)
	function := instance.LoadOne(
		code,
		fmt.Sprintf("%s_%d", prefix, id),
		64,
		16,
		[]bool{true, false},
		jitLocalPointers(),
		loader.Pcdata{{PC: uint32(len(code)), Val: 64}},
	)
	entry := **(**uintptr)(unsafe.Pointer(&function))
	flushJITCode(entry, len(code))
	return function
}

var jitLoader = loader.Loader{
	Name: "jjs.jit.",
	File: "github.com/nyan233/jjs-go/jit.go",
	Options: loader.Options{
		NoPreempt: true,
	},
}

type EncodeHandler func(buf *[]byte, val unsafe.Pointer) error

type DecodeHandler func(data []byte, val unsafe.Pointer) error
