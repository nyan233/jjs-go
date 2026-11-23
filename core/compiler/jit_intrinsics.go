package compiler

import (
	"errors"
	"math"
	"reflect"
	"runtime"
	"strconv"
	"unsafe"
)

const (
	jitHelperString = iota
	jitHelperInt
	jitHelperFloat
	jitHelperBool
	jitHelperNull
	jitHelperPointer
	jitHelperSlice
	jitHelperInterface
	jitHelperReserve
	jitHelperByteSlice
	jitHelperResolvedField
	jitHelperCustomMarshal
	jitHelperFieldPrefix
	jitHelperFloatFormat
	jitHelperYield
	jitHelperMapStart
	jitHelperMapNext
	jitHelperMapEnd
	jitHelperValue
	jitHelperQuote
	jitHelperQuoteTail
	jitHelperCount
)

type jitEncodeProgram struct {
	helpers     [jitHelperCount]uintptr
	static      []byte
	functions   []func(*jitEncodeContext) uintptr
	mapTypes    []reflect.Type
	fields      []jitEncodeFieldMeta
	customTypes []jitCustomTypeInfo
	digits      string
	base64      string
}

type jitEncodeFieldMeta struct {
	path       []jitFieldStep
	prefix     []byte
	childIndex int
	omitEmpty  bool
	arrayLen   int
	kind       reflect.Kind
	size       uintptr
	pointer    bool
	customKind jitCustomCallKind
	customType int
}

type jitEncodeContext struct {
	dst       []byte
	data      unsafe.Pointer
	program   *jitEncodeProgram
	err       error
	mapValue  unsafe.Pointer
	mapFrames *jitMapFrame
	mapActive *jitMapFrame
}

func newJITEncodeProgram(static []byte) *jitEncodeProgram {
	program := &jitEncodeProgram{static: static, digits: jitDigitPairs, base64: base64Alphabet}
	program.helpers[jitHelperString] = reflect.ValueOf(jitAppendString).Pointer()
	program.helpers[jitHelperInt] = reflect.ValueOf(jitAppendIntPtr).Pointer()
	program.helpers[jitHelperFloat] = reflect.ValueOf(jitAppendFloatPtr).Pointer()
	program.helpers[jitHelperBool] = reflect.ValueOf(jitAppendBoolPtr).Pointer()
	program.helpers[jitHelperNull] = reflect.ValueOf(jitAppendNull).Pointer()
	program.helpers[jitHelperPointer] = reflect.ValueOf(jitEncodePointer).Pointer()
	program.helpers[jitHelperSlice] = reflect.ValueOf(jitEncodeSlice).Pointer()
	program.helpers[jitHelperInterface] = reflect.ValueOf(jitEncodeInterface).Pointer()
	program.helpers[jitHelperReserve] = reflect.ValueOf(jitReserveRaw).Pointer()
	program.helpers[jitHelperByteSlice] = reflect.ValueOf(jitEncodeByteSlice).Pointer()
	program.helpers[jitHelperResolvedField] = reflect.ValueOf(jitEncodeResolvedField).Pointer()
	program.helpers[jitHelperCustomMarshal] = reflect.ValueOf(jitEncodeCustomMarshaler).Pointer()
	program.helpers[jitHelperFieldPrefix] = reflect.ValueOf(jitWriteFieldPrefix).Pointer()
	program.helpers[jitHelperFloatFormat] = reflect.ValueOf(strconv.AppendFloat).Pointer()
	program.helpers[jitHelperYield] = reflect.ValueOf(runtime.Gosched).Pointer()
	program.helpers[jitHelperMapStart] = reflect.ValueOf(jitMapStart).Pointer()
	program.helpers[jitHelperMapNext] = reflect.ValueOf((*reflect.MapIter).Next).Pointer()
	program.helpers[jitHelperMapEnd] = reflect.ValueOf(jitMapEnd).Pointer()
	program.helpers[jitHelperValue] = reflect.ValueOf(jitEncodeValue).Pointer()
	initJITQuoteHelpers(program)
	return program
}

func jitWriteFieldPrefix(ctx *jitEncodeContext, metaIndex int) {
	writeJITFieldPrefix(ctx, ctx.program.fields[metaIndex].prefix)
}

func jitEncodeResolvedField(ctx *jitEncodeContext, root unsafe.Pointer, metaIndex int) {
	if ctx == nil || ctx.program == nil || metaIndex < 0 || metaIndex >= len(ctx.program.fields) {
		return
	}
	meta := &ctx.program.fields[metaIndex]
	fieldPtr, ok := jitResolveField(root, meta.path, false)
	if !ok {
		return
	}
	if meta.omitEmpty && jitFieldEmpty(fieldPtr, meta.kind, meta.size, meta.arrayLen) {
		return
	}
	if meta.customKind != jitCustomNone {
		writeJITFieldPrefix(ctx, meta.prefix)
		jitEncodeCustomMarshaler(ctx, fieldPtr, int(meta.customKind), meta.customType)
		return
	}
	target := fieldPtr
	if meta.pointer {
		target = *(*unsafe.Pointer)(fieldPtr)
		if target == nil {
			writeJITFieldPrefix(ctx, meta.prefix)
			ctx.dst = append(ctx.dst, "null"...)
			return
		}
	}
	writeJITFieldPrefix(ctx, meta.prefix)
	if meta.childIndex < 0 || meta.childIndex >= len(ctx.program.functions) {
		ctx.setError(errors.New("json: invalid resolved field encoder index"))
		return
	}
	previous := ctx.data
	ctx.data = target
	ctx.program.functions[meta.childIndex](ctx)
	ctx.data = previous
}

func writeJITFieldPrefix(ctx *jitEncodeContext, prefix []byte) {
	ctx.ensureCapacity(len(prefix) + 1)
	if len(ctx.dst) > 0 && ctx.dst[len(ctx.dst)-1] != '{' {
		ctx.dst = append(ctx.dst, ',')
	}
	ctx.dst = append(ctx.dst, prefix...)
}

func jitFieldEmpty(fieldPtr unsafe.Pointer, kind reflect.Kind, size uintptr, arrayLen int) bool {
	switch kind {
	case reflect.Array:
		return arrayLen == 0
	case reflect.Map:
		hmap := *(*unsafe.Pointer)(fieldPtr)
		return hmap == nil || *(*int)(hmap) == 0
	case reflect.Slice, reflect.String:
		return *(*int)(unsafe.Add(fieldPtr, 8)) == 0
	case reflect.Interface, reflect.Pointer:
		return *(*unsafe.Pointer)(fieldPtr) == nil
	case reflect.Bool, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		switch size {
		case 1:
			return *(*uint8)(fieldPtr) == 0
		case 2:
			return *(*uint16)(fieldPtr) == 0
		case 4:
			return *(*uint32)(fieldPtr) == 0
		case 8:
			return *(*uint64)(fieldPtr) == 0
		}
	case reflect.Float32:
		return *(*float32)(fieldPtr) == 0
	case reflect.Float64:
		return *(*float64)(fieldPtr) == 0
	}
	return false
}

func (c *jitEncodeContext) setError(err error) {
	if err != nil && c.err == nil {
		c.err = err
	}
}

func (c *jitEncodeContext) ensureCapacity(additional int) {
	if additional <= 0 {
		return
	}
	required := len(c.dst) + additional
	if required <= cap(c.dst) {
		return
	}
	// Grow exponentially so a large output does not repeatedly copy the buffer.
	capacity := cap(c.dst) * 2
	if capacity < required {
		capacity = required
	}
	if capacity < 1024 {
		capacity = 1024
	}
	dst := make([]byte, len(c.dst), capacity)
	copy(dst, c.dst)
	c.dst = dst
}

//go:noinline
func jitReserveRaw(ctx *jitEncodeContext, n int) unsafe.Pointer {
	if ctx == nil || n <= 0 {
		return nil
	}
	start := len(ctx.dst)
	// The JIT may complete a short literal with one 64-bit store.
	ctx.ensureCapacity(n + 7)
	ctx.dst = ctx.dst[:start+n]
	return unsafe.Add(unsafe.Pointer(unsafe.SliceData(ctx.dst)), start)
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
	ctx.ensureCapacity(n + 2)
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
	lower, upper := 1e-6, 1e21
	if width == 4 {
		lower, upper = float64(float32(lower)), float64(float32(upper))
	}
	if abs != 0 && (abs < lower || abs >= upper) {
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

//go:noinline
func jitEncodeByteSlice(ctx *jitEncodeContext, headerPtr unsafe.Pointer) {
	ptr := *(*unsafe.Pointer)(headerPtr)
	length := *(*int)(unsafe.Add(headerPtr, 8))
	if ptr == nil {
		jitAppendNull(ctx)
		return
	}
	if length > (int(^uint(0)>>1)-2)/4*3 {
		ctx.setError(errors.New("json: byte slice too large"))
		return
	}
	n := (length+2)/3*4 + 2
	ctx.ensureCapacity(n)
	start := len(ctx.dst)
	ctx.dst = ctx.dst[:start+n]
	ctx.dst[start] = '"'
	encodeBase64(ctx.dst[start+1:start+n-1], unsafe.Slice((*byte)(ptr), length))
	ctx.dst[start+n-1] = '"'
}

const base64Alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
const base64ChunkBytes = 49152 // Multiple of 48; each chunk writes 64 KiB.

var (
	jitStringType  = unsafe.Pointer(typePointer(reflect.TypeOf("")))
	jitIntType     = unsafe.Pointer(typePointer(reflect.TypeOf(int(0))))
	jitBoolType    = unsafe.Pointer(typePointer(reflect.TypeOf(false)))
	jitFloat64Type = unsafe.Pointer(typePointer(reflect.TypeOf(float64(0))))
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
		typePtr = *(*unsafe.Pointer)(unsafe.Add(typeWord, 8))
	}
	// A nil named map can still have a value-receiver MarshalJSON method.
	// Nil pointers keep their normal null behavior; map dispatch must inspect
	// the actual type before deciding whether to invoke its marshaler.
	if data == nil && reflect.Kind((*runtimeTypeHeader)(typePtr).Kind&31) != reflect.Map {
		jitAppendNull(ctx)
		return
	}
	// Exact built-in types cannot have custom methods. Named types still use
	// the compiled path so json.Marshaler semantics remain unchanged.
	switch typePtr {
	case jitStringType:
		value := *(*string)(data)
		ctx.ensureCapacity(len(value) + 2)
		ctx.dst = appendQuoted(ctx.dst, value)
		return
	case jitIntType:
		ctx.dst = strconv.AppendInt(ctx.dst, int64(*(*int)(data)), 10)
		return
	case jitBoolType:
		ctx.dst = strconv.AppendBool(ctx.dst, *(*bool)(data))
		return
	case jitFloat64Type:
		jitAppendFloatPtr(ctx, data, 8)
		return
	}
	child, ok := jitEncoderPrograms.getCompileResult(uintptr(typePtr))
	if !ok {
		var value any
		dynamic := (*nonEmptyInterface)(unsafe.Pointer(&value))
		dynamic.tab, dynamic.data = typePtr, data
		var err error
		child, err = loadCompiledEncoder(reflect.TypeOf(value))
		if err != nil {
			ctx.setError(err)
			return
		}
	}
	// Nested encoders share the output and error state. Save the typed roots
	// across the call instead of borrowing another pooled context per value.
	previousData, previousProgram, previousMap := ctx.data, ctx.program, ctx.mapValue
	ctx.data, ctx.program = data, child.program
	if (child.rootKind == reflect.Map && !child.mapIndirect) || child.directInterface {
		ctx.mapValue = data
		ctx.data = unsafe.Pointer(&ctx.mapValue)
	}
	child.function(ctx)
	ctx.data, ctx.program, ctx.mapValue = previousData, previousProgram, previousMap
}
