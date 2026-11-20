package compiler

import (
	"github.com/bytedance/sonic/loader"
	"github.com/zbh255/bilog"
	"os"
	"unsafe"
)

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
