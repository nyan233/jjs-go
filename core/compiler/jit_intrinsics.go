package compiler

import (
	"encoding/base64"
	"errors"
	"math"
	"reflect"
	"strconv"
	"sync"
	"unsafe"
)

const jitEncodeStackSize = 4096

type jitEncodeProgram struct {
	helpers   [12]uintptr
	static    []byte
	functions []func(*jitEncodeContext) uintptr
	mapTypes  []uintptr
}

type jitEncodeContext struct {
	dst          []byte
	data         unsafe.Pointer
	stack        []byte
	program      *jitEncodeProgram
	err          error
	saved12      unsafe.Pointer
	saved13      uintptr
	saved9       uintptr
	saved15      uintptr
	savedScratch [8]uintptr
	slots        [256]uintptr
	slotTop      uintptr
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
	program.helpers[8] = reflect.ValueOf(jitEncodePointer).Pointer()
	program.helpers[9] = reflect.ValueOf(jitEncodeSlice).Pointer()
	program.helpers[10] = reflect.ValueOf(jitEncodeMap).Pointer()
	program.helpers[11] = reflect.ValueOf(jitEncodeInterface).Pointer()
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

//go:noinline
func jitEncodePointer(ctx *jitEncodeContext, fieldPtr unsafe.Pointer, childIndex int) {
	ptr := *(*unsafe.Pointer)(fieldPtr)
	if ptr == nil {
		jitAppendNull(ctx)
		return
	}
	if childIndex < 0 || childIndex >= len(ctx.program.functions) {
		ctx.setError(errors.New("json: invalid pointer encoder index"))
		return
	}
	previous := ctx.data
	ctx.data = ptr
	ctx.program.functions[childIndex](ctx)
	ctx.data = previous
}

//go:noinline
func jitEncodeSlice(ctx *jitEncodeContext, headerPtr unsafe.Pointer, childIndex, elemSize int) {
	ptr := *(*unsafe.Pointer)(headerPtr)
	length := *(*int)(unsafe.Add(headerPtr, 8))
	if ptr == nil {
		jitAppendNull(ctx)
		return
	}
	if childIndex < 0 || childIndex >= len(ctx.program.functions) {
		ctx.setError(errors.New("json: invalid slice encoder index"))
		return
	}
	ctx.dst = append(ctx.dst, '[')
	for i := 0; i < length; i++ {
		if i > 0 {
			ctx.dst = append(ctx.dst, ',')
		}
		previous := ctx.data
		ctx.data = unsafe.Add(ptr, uintptr(i*elemSize))
		ctx.program.functions[childIndex](ctx)
		ctx.data = previous
		if ctx.err != nil {
			return
		}
	}
	ctx.dst = append(ctx.dst, ']')
}

type jitMapIterator struct {
	key         unsafe.Pointer
	elem        unsafe.Pointer
	MapType     unsafe.Pointer
	hmap        unsafe.Pointer
	buckets     unsafe.Pointer
	bucketPtr   unsafe.Pointer
	overflow    unsafe.Pointer
	oldOverflow unsafe.Pointer
	startBucket uintptr
	offset      uint8
	wrapped     bool
	B           uint8
	i           uint8
	_           uint8
	bucket      uintptr
	checkBucket uintptr
}

//go:noinline
func jitEncodeMap(ctx *jitEncodeContext, mapFieldPtr unsafe.Pointer, keyIndex, valueIndex, typeIndex int) {
	mapPtr := *(*unsafe.Pointer)(mapFieldPtr)
	if mapPtr == nil {
		jitAppendNull(ctx)
		return
	}
	if typeIndex < 0 || typeIndex >= len(ctx.program.mapTypes) {
		ctx.setError(errors.New("json: invalid map type index"))
		return
	}
	var iterator jitMapIterator
	runtimeMapIterInit(unsafe.Pointer(ctx.program.mapTypes[typeIndex]), mapPtr, unsafe.Pointer(&iterator))
	ctx.dst = append(ctx.dst, '{')
	written := 0
	for iterator.key != nil {
		if written > 0 {
			ctx.dst = append(ctx.dst, ',')
		}
		previous := ctx.data
		ctx.data = iterator.key
		ctx.program.functions[keyIndex](ctx)
		if ctx.err != nil {
			ctx.data = previous
			return
		}
		ctx.dst = append(ctx.dst, ':')
		ctx.data = iterator.elem
		ctx.program.functions[valueIndex](ctx)
		ctx.data = previous
		if ctx.err != nil {
			return
		}
		written++
		runtimeMapIterNext(unsafe.Pointer(&iterator))
	}
	ctx.dst = append(ctx.dst, '}')
}

//go:linkname runtimeMapIterInit runtime.mapiterinit
func runtimeMapIterInit(mapType, hmap, iterator unsafe.Pointer)

//go:linkname runtimeMapIterNext runtime.mapiternext
func runtimeMapIterNext(iterator unsafe.Pointer)

var (
	jitDynamicEncoderCache sync.Map
	jitDynamicCompileMu    sync.Mutex
)

//go:noinline
func jitEncodeInterface(ctx *jitEncodeContext, interfacePtr unsafe.Pointer, nonEmpty int) {
	typeWord := *(*unsafe.Pointer)(interfacePtr)
	data := *(*unsafe.Pointer)(unsafe.Add(interfacePtr, 8))
	if typeWord == nil {
		jitAppendNull(ctx)
		return
	}
	typePtr := typeWord
	if nonEmpty != 0 {
		// runtime.itab: *abi.InterfaceType, *abi.Type
		typePtr = *(*unsafe.Pointer)(unsafe.Add(typeWord, 8))
	}
	var value any
	dynamic := (*nonEmptyInterface)(unsafe.Pointer(&value))
	dynamic.tab = typePtr
	dynamic.data = data
	typ := reflect.TypeOf(value)
	if typ == nil {
		ctx.setError(errors.New("json: invalid interface type"))
		return
	}
	encoded, ok := jitDynamicEncoderCache.Load(uintptr(typePtr))
	var child *jitCompiledEncoder
	if ok {
		child = encoded.(*jitCompiledEncoder)
	} else {
		jitDynamicCompileMu.Lock()
		defer jitDynamicCompileMu.Unlock()
		if encoded, ok := jitDynamicEncoderCache.Load(uintptr(typePtr)); ok {
			child = encoded.(*jitCompiledEncoder)
		} else {
			compiled, err := compileJITEncoder(typ)
			if err != nil {
				ctx.setError(err)
				return
			}
			actual, _ := jitDynamicEncoderCache.LoadOrStore(uintptr(typePtr), compiled)
			child = actual.(*jitCompiledEncoder)
		}
	}
	childContext := &jitEncodeContext{
		dst:     ctx.dst,
		data:    data,
		program: child.program,
	}
	jitEncodeContexts.Store(childContext, struct{}{})
	defer jitEncodeContexts.Delete(childContext)
	child.function(childContext)
	ctx.dst = childContext.dst
	ctx.setError(childContext.err)
}

var _ = jitEncodeStackSize
