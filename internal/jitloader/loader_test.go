package jitloader

import (
	"runtime"
	"testing"
	"unsafe"
)

func TestLoadOne(t *testing.T) {
	var code []byte
	switch runtime.GOARCH {
	case "amd64":
		code = []byte{
			0xb8, 0x2a, 0x00, 0x00, 0x00, // MOVL $42, AX
			0xc3, // RET
		}
	case "arm64":
		code = []byte{
			0x40, 0x05, 0x80, 0xd2, // MOVZ X0, #42
			0xc0, 0x03, 0x5f, 0xd6, // RET
		}
	default:
		t.Skipf("unsupported test architecture %s", runtime.GOARCH)
	}

	loader := Loader{
		Name: "jitloader.test.",
		File: "github.com/nyan233/jjs-go/internal/jitloader/loader_test.go",
		Options: Options{
			NoPreempt: true,
		},
	}
	for i := 0; i < 2; i++ {
		loaded := loader.LoadOne(
			code,
			"answer",
			0,
			0,
			nil,
			nil,
			Pcdata{{PC: uint32(len(code)), Val: 0}},
		)
		fn := *(*func() int)(unsafe.Pointer(&loaded))
		if got := fn(); got != 42 {
			t.Fatalf("loaded function returned %d, want 42", got)
		}
		runtime.GC()
		runtime.KeepAlive(loaded)
	}
}
