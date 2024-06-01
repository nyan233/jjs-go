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
		File: "github.com/nyan233/jjs/jit.go",
		Options: loader.Options{
			NoPreempt: true,
		},
	}
)

type EncodeHandler func(stack unsafe.Pointer, buf *[]byte, val unsafe.Pointer, opt uint64)

type DecodeHandler func(stack unsafe.Pointer, ast unsafe.Pointer, val unsafe.Pointer)
