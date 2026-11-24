package compiler

import (
	"cmp"
	"encoding"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"unsafe"

	"github.com/twitchyliquid64/golang-asm/obj"
)

type jitMapEntry struct {
	name       string
	valueIndex int
}

// Frames are retained by the pooled encoder context. Entries are cleared at
// mapEnd so the backing arrays can be reused without pinning map values.
type jitMapFrame struct {
	iterator   reflect.MapIter
	entries    []jitMapEntry
	names      []byte
	values     reflect.Value
	valueCount int
	name       string
	elem       unsafe.Pointer
	index      int
	parent     *jitMapFrame
	next       *jitMapFrame
}

type jitMapIteratorLayout struct {
	name, elem int64
	valid      bool
}

type jitRawMapIteratorLayout struct {
	key, elem int64
	valid     bool
}

var jitMapLayout = jitMapIteratorLayout{
	name:  int64(unsafe.Offsetof(jitMapFrame{}.name)),
	elem:  int64(unsafe.Offsetof(jitMapFrame{}.elem)),
	valid: true,
}

var jitRawMapLayout = inspectJITMapIterator()

func inspectJITMapIterator() jitRawMapIteratorLayout {
	if unsafe.Offsetof(jitMapFrame{}.iterator) != 0 {
		return jitRawMapIteratorLayout{}
	}
	iterator := reflect.TypeOf(reflect.MapIter{})
	state, ok := iterator.FieldByName("hiter")
	if !ok || state.Type.Kind() != reflect.Struct {
		return jitRawMapIteratorLayout{}
	}
	key, okKey := state.Type.FieldByName("key")
	elem, okElem := state.Type.FieldByName("elem")
	if !okKey || !okElem || key.Type.Kind() != reflect.UnsafePointer || elem.Type.Kind() != reflect.UnsafePointer ||
		key.Offset != 0 || elem.Offset != unsafe.Sizeof(unsafe.Pointer(nil)) || state.Offset+elem.Offset+elem.Type.Size() > iterator.Size() {
		return jitRawMapIteratorLayout{}
	}
	base := unsafe.Offsetof(jitMapFrame{}.iterator) + state.Offset
	return jitRawMapIteratorLayout{key: int64(base + key.Offset), elem: int64(base + elem.Offset), valid: true}
}

//go:noinline
func jitMapStart(ctx *jitEncodeContext, fieldPtr unsafe.Pointer, typeIndex int) {
	var frame *jitMapFrame
	if ctx.mapActive == nil {
		if ctx.mapFrames == nil {
			ctx.mapFrames = new(jitMapFrame)
		}
		frame = ctx.mapFrames
	} else {
		if ctx.mapActive.next == nil {
			ctx.mapActive.next = &jitMapFrame{parent: ctx.mapActive}
		}
		frame = ctx.mapActive.next
	}
	ctx.mapActive = frame
	frame.resetEntries()

	// Refer to the existing map field/header; boxing a local copy of its map
	// pointer here would reintroduce an allocation for every map traversal.
	value := reflect.NewAt(ctx.program.mapTypes[typeIndex], fieldPtr).Elem()
	keyType, elemType := value.Type().Key(), value.Type().Elem()
	elemTypePtr := runtimeTypePointer(elemType)
	sliceType := reflect.SliceOf(elemType)
	length := value.Len()
	if !frame.values.IsValid() || frame.values.Type() != sliceType || frame.values.Cap() < length {
		frame.values = reflect.MakeSlice(sliceType, length, length)
	}
	frame.valueCount = 0
	frame.iterator.Reset(value)
	for frame.iterator.Next() {
		key := *(*unsafe.Pointer)(unsafe.Add(unsafe.Pointer(frame), jitRawMapLayout.key))
		name, err := frame.resolveName(key, keyType)
		if err != nil {
			ctx.setError(fmt.Errorf("json: encoding error for type %q: %q", value.Type().String(), err.Error()))
			return
		}
		// Resolve the key before reading the element pointer: a TextMarshaler
		// callback may grow the map and update the iterator's element cursor.
		elem := *(*unsafe.Pointer)(unsafe.Add(unsafe.Pointer(frame), jitRawMapLayout.elem))
		index := frame.valueCount
		frame.ensureValueSlot(index)
		frame.valueCount++
		runtimeTypedMemmove(
			elemTypePtr,
			unsafe.Pointer(frame.values.Index(index).UnsafeAddr()),
			elem,
		)
		frame.entries = append(frame.entries, jitMapEntry{name: name, valueIndex: index})
	}
	slices.SortFunc(frame.entries, func(a, b jitMapEntry) int {
		return cmp.Compare(a.name, b.name)
	})
	frame.index = -1
	jitMapNext(ctx)
}

func (frame *jitMapFrame) ensureValueSlot(index int) {
	if index < frame.values.Cap() {
		return
	}
	capacity := max(index+1, frame.values.Cap()*2)
	values := reflect.MakeSlice(frame.values.Type(), capacity, capacity)
	elemType := runtimeTypePointer(frame.values.Type().Elem())
	for slot := 0; slot < frame.valueCount; slot++ {
		runtimeTypedMemmove(
			elemType,
			unsafe.Pointer(values.Index(slot).UnsafeAddr()),
			unsafe.Pointer(frame.values.Index(slot).UnsafeAddr()),
		)
	}
	frame.values = values
}

func (frame *jitMapFrame) resetEntries() {
	clear(frame.entries)
	frame.entries = frame.entries[:0]
	frame.names = frame.names[:0]
	frame.index = 0
	frame.clearCurrent()
}

func (frame *jitMapFrame) resolveName(key unsafe.Pointer, keyType reflect.Type) (string, error) {
	if jitJSONV2Semantics && keyType.Implements(jitTextMarshalerType) {
		return frame.resolveTextName(key, keyType)
	}
	if keyType.Kind() == reflect.String {
		return *(*string)(key), nil
	}
	if keyType.Implements(jitTextMarshalerType) {
		return frame.resolveTextName(key, keyType)
	}
	switch keyType.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		start := len(frame.names)
		frame.names = strconv.AppendInt(frame.names, loadMapKeyInt(key, keyType.Bits()), 10)
		return unsafe.String(&frame.names[start], len(frame.names)-start), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		start := len(frame.names)
		frame.names = strconv.AppendUint(frame.names, loadMapKeyUint(key, keyType.Bits()), 10)
		return unsafe.String(&frame.names[start], len(frame.names)-start), nil
	case reflect.Float32, reflect.Float64:
		if !jitJSONV2Semantics {
			panic("json: unexpected map key type")
		}
		start := len(frame.names)
		value := loadMapKeyFloat(key, keyType.Bits())
		formatted, err := appendJSONFloat(frame.names, value, keyType.Bits()/8)
		frame.names = formatted
		if err != nil {
			return "", err
		}
		return unsafe.String(&frame.names[start], len(frame.names)-start), nil
	default:
		panic("json: unexpected map key type")
	}
}

func (frame *jitMapFrame) resolveTextName(key unsafe.Pointer, keyType reflect.Type) (string, error) {
	value := reflect.NewAt(keyType, key).Elem()
	marshaler := value.Interface().(encoding.TextMarshaler)
	if keyType.Kind() == reflect.Pointer && value.IsNil() {
		return "", nil
	}
	text, err := marshaler.MarshalText()
	if err != nil {
		return "", err
	}
	return frame.copyName(text), nil
}

func loadMapKeyInt(ptr unsafe.Pointer, bits int) int64 {
	switch bits {
	case 8:
		return int64(*(*int8)(ptr))
	case 16:
		return int64(*(*int16)(ptr))
	case 32:
		return int64(*(*int32)(ptr))
	case 64:
		return *(*int64)(ptr)
	default:
		panic("json: unexpected signed map-key width")
	}
}

func loadMapKeyUint(ptr unsafe.Pointer, bits int) uint64 {
	switch bits {
	case 8:
		return uint64(*(*uint8)(ptr))
	case 16:
		return uint64(*(*uint16)(ptr))
	case 32:
		return uint64(*(*uint32)(ptr))
	case 64:
		return *(*uint64)(ptr)
	default:
		panic("json: unexpected unsigned map-key width")
	}
}

func loadMapKeyFloat(ptr unsafe.Pointer, bits int) float64 {
	if bits == 32 {
		return float64(*(*float32)(ptr))
	}
	return *(*float64)(ptr)
}

func (frame *jitMapFrame) copyName(text []byte) string {
	if len(text) == 0 {
		return ""
	}
	start := len(frame.names)
	frame.names = append(frame.names, text...)
	return unsafe.String(&frame.names[start], len(text))
}

func (frame *jitMapFrame) clearCurrent() {
	frame.name = ""
	frame.elem = nil
}

//go:noinline
func jitMapNext(ctx *jitEncodeContext) bool {
	frame := ctx.mapActive
	if frame == nil {
		return false
	}
	frame.index++
	if frame.index >= len(frame.entries) {
		frame.clearCurrent()
		return false
	}
	entry := &frame.entries[frame.index]
	frame.name = entry.name
	frame.elem = unsafe.Pointer(frame.values.Index(entry.valueIndex).UnsafeAddr())
	return true
}

//go:noinline
func jitMapEnd(ctx *jitEncodeContext) {
	frame := ctx.mapActive
	frame.clearCurrent()
	frame.iterator.Reset(reflect.Value{})
	if frame.values.IsValid() {
		elemType := runtimeTypePointer(frame.values.Type().Elem())
		for index := 0; index < frame.valueCount; index++ {
			runtimeTypedMemclr(
				elemType,
				unsafe.Pointer(frame.values.Index(index).UnsafeAddr()),
			)
		}
	}
	frame.valueCount = 0
	clear(frame.entries)
	frame.entries = frame.entries[:0]
	frame.names = frame.names[:0]
	ctx.mapActive = frame.parent
}

func (ctx *jitEncodeContext) releaseMaps() {
	for ctx.mapActive != nil {
		jitMapEnd(ctx)
	}
}

// Recursive or oversized values get their own generated unit. This bridge
// performs the heap pointer write with Go's write barrier and supplies a Go
// stack check before calling the child. Small map values and pointer targets
// are emitted inline and do not use it.
//
//go:noinline
func jitEncodeValue(ctx *jitEncodeContext, ptr unsafe.Pointer, index int) {
	previous := ctx.data
	ctx.data = ptr
	ctx.program.functions[index](ctx)
	ctx.data = previous
}

func (g *jitEncoderGenerator) emitOutlinedValue(typ reflect.Type, offset int64, addressable bool) error {
	index, err := g.compiler.unitIndex(typ, addressable)
	if err != nil {
		return err
	}
	g.flushRaw()
	g.code.mov(_R10, _RAX)
	g.code.lea(_R12, offset, _RBX)
	g.code.movConst(int64(index), _RCX)
	g.emitHelper(jitHelperValue)
	return nil
}

func (g *jitEncoderGenerator) emitMap(typ reflect.Type, offset int64) error {
	key := typ.Key()
	if !supportsJITMapKeyEncode(key) {
		return fmt.Errorf("json: JIT encoder does not support map key type %s", key)
	}
	if !jitMapLayout.valid {
		return fmt.Errorf("json: unsupported reflect.MapIter layout")
	}
	if !jitRawMapLayout.valid {
		return fmt.Errorf("json: unsupported reflect.MapIter pointer layout")
	}
	if g.depth == jitInlineDepth {
		return g.emitOutlinedValue(typ, offset, g.addressable)
	}
	c := g.code
	nilMap, closeMap, loop, done := c.newLabel(), c.newLabel(), c.newLabel(), c.newLabel()
	c.loadMem(_R12, offset, _RBX)
	c.compareConst(0, _RBX)
	c.branch(jitEqual, nilMap)
	g.pushRoot()
	c.mov(_R10, _RAX)
	c.lea(_R12, offset, _RBX)
	c.movConst(int64(g.compiler.mapTypeIndex(typ)), _RCX)
	g.emitHelper(jitHelperMapStart)
	g.emitRaw([]byte("{"))
	g.flushRaw()
	g.loadMapFrame(_RAX)
	c.loadMem(_RAX, jitMapLayout.elem, _RBX)
	c.compareConst(0, _RBX)
	c.branch(jitEqual, closeMap)
	c.bind(loop)
	g.loadMapFrame(_RAX)
	// The data slot is only one word wide; the string header needs its own
	// scratch space so its length cannot overwrite the saved context pointer.
	c.lea(_RSP, c.scratchSlot(), _R12)
	c.loadMem(_RAX, jitMapLayout.name, _RCX)
	c.mov(_RCX, mem(_R12, 0))
	c.loadMem(_RAX, jitMapLayout.name+8, _RCX)
	c.mov(_RCX, mem(_R12, 8))
	// emitHelper restores R12 from dataSlot after the quote assembly call.
	c.mov(_R12, mem(_RSP, c.dataSlot()))
	g.emitString(0)
	g.emitRaw([]byte(":"))
	g.flushRaw()
	g.loadMapFrame(_RAX)
	c.loadMem(_RAX, jitMapLayout.elem, _R12)
	c.mov(_R12, mem(_RSP, c.dataSlot()))
	var err error
	if g.canInline(typ.Elem()) {
		previous := g.addressable
		g.addressable = false // A map element is not addressable in encoding/json.
		err = g.emitValue(typ.Elem(), 0)
		g.addressable = previous
	} else {
		err = g.emitOutlinedValue(typ.Elem(), 0, false)
	}
	if err != nil {
		return err
	}
	g.flushRaw()
	c.loadMem(_R10, int64(unsafe.Offsetof(jitEncodeContext{}.err)), _RBX)
	c.compareConst(0, _RBX)
	c.branch(jitNotEqual, g.returnLabel)
	c.mov(_R10, _RAX)
	g.emitHelper(jitHelperMapNext)
	// Values are captured before sorting, matching encoding/json when a key or
	// value callback mutates the map during encoding.
	// A Go bool result occupies one byte. amd64 may only set AL, leaving
	// arbitrary upper bits in AX; do not interpret those as part of the result.
	c.mask(255, _RAX)
	c.compareConst(0, _RAX)
	c.branch(jitEqual, closeMap)
	g.emitRaw([]byte(","))
	g.flushRaw()
	c.branch(jitAlways, loop)
	c.bind(closeMap)
	g.emitRaw([]byte("}"))
	g.flushRaw()
	c.mov(_R10, _RAX)
	g.emitHelper(jitHelperMapEnd)
	g.popRoot()
	c.branch(jitAlways, done)
	c.bind(nilMap)
	g.emitRaw([]byte("null"))
	g.flushRaw()
	c.bind(done)
	return nil
}

func (g *jitEncoderGenerator) loadMapFrame(to obj.Addr) {
	g.code.loadMem(_R10, int64(unsafe.Offsetof(jitEncodeContext{}.mapActive)), to)
}

//go:linkname runtimeTypedMemmove reflect.typedmemmove
func runtimeTypedMemmove(typ, dst, src unsafe.Pointer)
