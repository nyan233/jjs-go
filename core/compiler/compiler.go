package compiler

import (
	"github.com/zbh255/bilog"
	"os"
	"reflect"
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
)

// EncodeHandler appends the JSON representation of val to buf.
//
// The handler is compiled for a specific reflect.Type. reflect.Value is used
// instead of an unsafe data pointer so unsupported values and cycles can be
// reported as errors instead of corrupting memory.
type EncodeHandler func(buf *[]byte, val reflect.Value) error

// DecodeHandler decodes JSON into the addressable value val.
type DecodeHandler func(data []byte, val reflect.Value) error
