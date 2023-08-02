package jjs_go

import (
	"errors"
	"github.com/nyan233/jjs-go/internal/asm"
	"github.com/nyan233/jjs-go/internal/mempool"
	"github.com/nyan233/jjs-go/internal/rtype"
	"reflect"
	"runtime"
	"unsafe"
)

func Marshal(data interface{}) ([]byte, error) {
	if data == nil {
		return nil, errors.New("data is nil")
	}
	ef := (*rtype.EmptyFace)(unsafe.Pointer(&data))
	pg := go_GobalJitCompiler.Compile(reflect.TypeOf(data), true)
	if pg == nil {
		panic("program is null")
	}
	// TODO grow
	newBytes := make([]byte, pg.MinSize+1024*16, pg.MinSize+1024*16)
	stack := mempool.StackPool.Get().(*mempool.Stack)
	write, reason := asm.CallMFunc(pg.MFunc, uintptr(stack.High), unsafe.Pointer(&newBytes[0]), ef.Val)
	newBytes = newBytes[:write+pg.MinSize]
	_ = reason
	runtime.KeepAlive(stack)
	runtime.KeepAlive(newBytes)
	runtime.KeepAlive(ef.Val)
	return newBytes, nil
}

func Unmarshal(bytes []byte, data interface{}) error {
	if data == nil {
		return errors.New("data is nil")
	}
	return nil
}
