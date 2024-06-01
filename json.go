package jjs_go

import (
	"errors"
	"github.com/nyan233/jjs-go/core/compiler"
	"github.com/nyan233/jjs-go/core/mempool"
	"github.com/nyan233/jjs-go/internal/rtype"
	"reflect"
	"runtime"
	"unsafe"
)

func Marshal(data interface{}) (buf []byte, err error) {
	if data == nil {
		return nil, errors.New("data is nil")
	}
	ef := (*rtype.EmptyFace)(unsafe.Pointer(&data))
	function, err := compiler.CompileEncoder(reflect.TypeOf(data), 0)
	if err != nil {
		panic(err)
	}
	// TODO grow
	newBytes := make([]byte, 0, 512)
	stack := mempool.StackPool.Get().(*mempool.Stack)
	defer func() {
		rErr := recover()
		if rErr != nil {
			err, _ = rErr.(error)
		}
	}()
	function(stack.High, &newBytes, ef.Val, 0)
	runtime.KeepAlive(stack)
	runtime.KeepAlive(&newBytes)
	runtime.KeepAlive(ef.Val)
	return newBytes, nil
}

func Unmarshal(bytes []byte, data interface{}) error {
	if data == nil {
		return errors.New("data is nil")
	}
	return nil
}
