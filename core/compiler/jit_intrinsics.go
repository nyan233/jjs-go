package compiler

import (
	"encoding/base64"
	"errors"
	"math"
	"reflect"
	"strconv"
	"unsafe"
)

const jitEncodeStackSize = 4096

type jitEncodeProgram struct {
	helpers [8]uintptr
	static  []byte
}

type jitEncodeContext struct {
	dst     []byte
	data    unsafe.Pointer
	stack   []byte
	program *jitEncodeProgram
	err     error
	saved12 unsafe.Pointer
	saved13 uintptr
	saved9  uintptr
	saved15 uintptr
	slots   [256]uintptr
	slotTop uintptr
}

func newJITEncodeProgram(static []byte) *jitEncodeProgram {
	program := &jitEncodeProgram{static: static}
	program.helpers[0] = reflect.ValueOf(jitAppendRaw).Pointer()
	program.helpers[1] = reflect.ValueOf(jitAppendString).Pointer()
	program.helpers[2] = reflect.ValueOf(jitAppendIntPtr).Pointer()
	program.helpers[3] = reflect.ValueOf(jitAppendFloatPtr).Pointer()
	program.helpers[4] = reflect.ValueOf(jitAppendBoolPtr).Pointer()
	program.helpers[5] = reflect.ValueOf(jitAppendNull).Pointer()
	program.helpers[6] = reflect.ValueOf(jitIsEmpty).Pointer()
	program.helpers[7] = reflect.ValueOf(jitAppendBase64).Pointer()
	return program
}

func (c *jitEncodeContext) setError(err error) {
	if err != nil && c.err == nil {
		c.err = err
	}
}

//go:noinline
func jitAppendRaw(ctx *jitEncodeContext, ptr unsafe.Pointer, n int) {
	if ctx == nil || n < 0 {
		return
	}
	if n > 0 && ptr == nil {
		ctx.setError(errors.New("json: nil source buffer"))
		return
	}
	ctx.dst = append(ctx.dst, unsafe.Slice((*byte)(ptr), n)...)
}

//go:noinline
func jitAppendString(ctx *jitEncodeContext, ptr unsafe.Pointer, n int) {
	if ctx == nil || n < 0 {
		return
	}
	if n > 0 && ptr == nil {
		ctx.setError(errors.New("json: nil string pointer"))
		return
	}
	ctx.dst = appendQuoted(ctx.dst, unsafe.String((*byte)(ptr), n))
}

//go:noinline
func jitAppendIntPtr(ctx *jitEncodeContext, ptr unsafe.Pointer, width, signed int) {
	if ctx == nil || ptr == nil {
		return
	}
	switch width {
	case 1:
		if signed != 0 {
			ctx.dst = strconv.AppendInt(ctx.dst, int64(*(*int8)(ptr)), 10)
		} else {
			ctx.dst = strconv.AppendUint(ctx.dst, uint64(*(*uint8)(ptr)), 10)
		}
	case 2:
		if signed != 0 {
			ctx.dst = strconv.AppendInt(ctx.dst, int64(*(*int16)(ptr)), 10)
		} else {
			ctx.dst = strconv.AppendUint(ctx.dst, uint64(*(*uint16)(ptr)), 10)
		}
	case 4:
		if signed != 0 {
			ctx.dst = strconv.AppendInt(ctx.dst, int64(*(*int32)(ptr)), 10)
		} else {
			ctx.dst = strconv.AppendUint(ctx.dst, uint64(*(*uint32)(ptr)), 10)
		}
	case 8:
		if signed != 0 {
			ctx.dst = strconv.AppendInt(ctx.dst, *(*int64)(ptr), 10)
		} else {
			ctx.dst = strconv.AppendUint(ctx.dst, *(*uint64)(ptr), 10)
		}
	default:
		ctx.setError(errors.New("json: unsupported integer width"))
	}
}

//go:noinline
func jitAppendFloatPtr(ctx *jitEncodeContext, ptr unsafe.Pointer, width int) {
	if ctx == nil || ptr == nil {
		return
	}
	var value float64
	switch width {
	case 4:
		value = float64(*(*float32)(ptr))
	case 8:
		value = *(*float64)(ptr)
	default:
		ctx.setError(errors.New("json: unsupported float width"))
		return
	}
	if math.IsNaN(value) || math.IsInf(value, 0) {
		ctx.setError(errors.New("json: unsupported non-finite float"))
		return
	}
	format := byte('f')
	abs := math.Abs(value)
	if abs != 0 && (abs < 1e-6 || abs >= 1e21) {
		format = 'e'
	}
	ctx.dst = strconv.AppendFloat(ctx.dst, value, format, -1, width*8)
	if format == 'e' {
		if n := len(ctx.dst); n >= 4 && ctx.dst[n-4] == 'e' && ctx.dst[n-3] == '-' && ctx.dst[n-2] == '0' {
			ctx.dst[n-2] = ctx.dst[n-1]
			ctx.dst = ctx.dst[:n-1]
		}
	}
}

//go:noinline
func jitAppendBoolPtr(ctx *jitEncodeContext, ptr unsafe.Pointer) {
	if ctx == nil || ptr == nil {
		return
	}
	ctx.dst = strconv.AppendBool(ctx.dst, *(*uint8)(ptr) != 0)
}

//go:noinline
func jitAppendNull(ctx *jitEncodeContext) {
	if ctx != nil {
		ctx.dst = append(ctx.dst, "null"...)
	}
}

//go:noinline
func jitAppendBase64(ctx *jitEncodeContext, ptr unsafe.Pointer, n int) {
	if ctx == nil || n < 0 {
		return
	}
	if n > 0 && ptr == nil {
		ctx.setError(errors.New("json: nil byte slice"))
		return
	}
	ctx.dst = append(ctx.dst, '"')
	dstLen := len(ctx.dst)
	ctx.dst = append(ctx.dst, make([]byte, base64.StdEncoding.EncodedLen(n))...)
	base64.StdEncoding.Encode(ctx.dst[dstLen:], unsafe.Slice((*byte)(ptr), n))
	ctx.dst = append(ctx.dst, '"')
}

const (
	jitEmptyString = iota
	jitEmptyBool
	jitEmptyInt
	jitEmptyUint
	jitEmptyFloat
	jitEmptyPointer
	jitEmptySlice
)

//go:noinline
func jitIsEmpty(_ *jitEncodeContext, ptr unsafe.Pointer, kind, _ int) int {
	switch kind {
	case jitEmptyString, jitEmptySlice:
		if *(*unsafe.Pointer)(ptr) == nil {
			return 1
		}
		return boolInt(*(*int)(unsafe.Add(ptr, 8)) == 0)
	case jitEmptyPointer:
		return boolInt(*(*unsafe.Pointer)(ptr) == nil)
	case jitEmptyBool:
		return boolInt(*(*uint8)(ptr) == 0)
	case jitEmptyInt:
		return boolInt(*(*int64)(ptr) == 0)
	case jitEmptyUint:
		return boolInt(*(*uint64)(ptr) == 0)
	case jitEmptyFloat:
		return boolInt(*(*float64)(ptr) == 0)
	default:
		return 0
	}
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

var _ = jitEncodeStackSize
