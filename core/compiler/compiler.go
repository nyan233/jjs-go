package compiler

import (
	"fmt"
	"github.com/bytedance/sonic/loader"
	"github.com/zbh255/bilog"
	"os"
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
	return instance.LoadOne(code, fmt.Sprintf("%s_%d", prefix, id), 64, 16, []bool{true, false}, []bool{true, false, false, false, false, false, false, false})
}

const (
	Debug         = false
	JIT_MAX_STACK = 1 << 12
)

var (
	logger = bilog.NewLogger(os.Stdout, bilog.PANIC,
		bilog.WithTopBuffer(0),
		bilog.WithLowBuffer(0),
		bilog.WithTimes(),
		bilog.WithCaller(0))

	jitLoader = loader.Loader{
		Name: "jjs.jit.",
		File: "github.com/nyan233/jjs-go/jit.go",
		Options: loader.Options{
			NoPreempt: true,
		},
	}
)

type EncodeHandler func(buf *[]byte, val unsafe.Pointer) error

type DecodeHandler func(data []byte, val unsafe.Pointer) error
