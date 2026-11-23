package compiler

import (
	"fmt"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/nyan233/jjs-go/internal/jitloader"
)

var jitFunctionID atomic.Uint64
var jitTypeRegistry sync.Map

func loadJITFunction(code []byte, prefix string, pcsp jitloader.Pcdata) jitloader.Function {
	return loadJITFunctionFrame(code, prefix, pcsp, jitFrameSize, jitLocalPointers())
}

func loadJITFunctionFrame(code []byte, prefix string, pcsp jitloader.Pcdata, frameSize int, pointers []bool) jitloader.Function {
	id := jitFunctionID.Add(1)
	instance := jitLoader
	instance.Name = fmt.Sprintf("jjs.jit.%d.", id)
	function := instance.LoadOne(
		code,
		fmt.Sprintf("%s_%d", prefix, id),
		frameSize,
		0, // arguments/results are in registers; incoming spill slots are uninitialized
		nil,
		pointers,
		pcsp,
	)
	entry := **(**uintptr)(unsafe.Pointer(&function))
	flushJITCode(entry, len(code))
	return function
}

var jitLoader = jitloader.Loader{
	Name: "jjs.jit.",
	File: "github.com/nyan233/jjs-go/jit.go",
	Options: jitloader.Options{
		NoPreempt: true,
	},
}

type EncodeHandler func(buf *[]byte, val unsafe.Pointer) error

type DecodeHandler func(data []byte, val unsafe.Pointer) error
