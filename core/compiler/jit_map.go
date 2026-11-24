package compiler

import (
	"fmt"
	"reflect"
	"unsafe"

	"github.com/twitchyliquid64/golang-asm/obj"
)

// Keep the real reflect iterator so its storage and GC bitmap always match
// the running Go toolchain. The nodes have stable addresses across recursive
// growth, and are retained by the pooled context only after Reset clears maps.
type jitMapFrame struct {
	iterator reflect.MapIter
	parent   *jitMapFrame
	next     *jitMapFrame
}

type jitMapIteratorLayout struct {
	key, elem int64
	valid     bool
}

var jitMapLayout = inspectJITMapIterator()

func inspectJITMapIterator() jitMapIteratorLayout {
	if unsafe.Offsetof(jitMapFrame{}.iterator) != 0 {
		return jitMapIteratorLayout{}
	}
	iterator := reflect.TypeOf(reflect.MapIter{})
	state, ok := iterator.FieldByName("hiter")
	if !ok || state.Type.Kind() != reflect.Struct {
		return jitMapIteratorLayout{}
	}
	key, okKey := state.Type.FieldByName("key")
	elem, okElem := state.Type.FieldByName("elem")
	if !okKey || !okElem || key.Type.Kind() != reflect.UnsafePointer || elem.Type.Kind() != reflect.UnsafePointer ||
		key.Offset != 0 || elem.Offset != unsafe.Sizeof(unsafe.Pointer(nil)) || state.Offset+elem.Offset+elem.Type.Size() > iterator.Size() {
		return jitMapIteratorLayout{}
	}
	base := unsafe.Offsetof(jitMapFrame{}.iterator) + state.Offset
	return jitMapIteratorLayout{key: int64(base + key.Offset), elem: int64(base + elem.Offset), valid: true}
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
	// Refer to the existing map field/header; boxing a local copy of its map
	// pointer here would reintroduce an allocation for every map traversal.
	frame.iterator.Reset(reflect.NewAt(ctx.program.mapTypes[typeIndex], fieldPtr).Elem())
	frame.iterator.Next()
}

//go:noinline
func jitMapEnd(ctx *jitEncodeContext) {
	frame := ctx.mapActive
	frame.iterator.Reset(reflect.Value{})
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
	switch key.Kind() {
	case reflect.String, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
	default:
		return fmt.Errorf("json: JIT encoder does not support map key type %s", key)
	}
	if !jitMapLayout.valid {
		return fmt.Errorf("json: unsupported reflect.MapIter layout")
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
	c.loadMem(_RAX, jitMapLayout.key, _RBX)
	c.compareConst(0, _RBX)
	c.branch(jitEqual, closeMap)
	c.bind(loop)
	g.loadMapFrame(_RAX)
	c.loadMem(_RAX, jitMapLayout.key, _R12)
	c.mov(_R12, mem(_RSP, c.dataSlot()))
	// Map keys use their declared scalar type, not value MarshalJSON methods.
	if key.Kind() == reflect.String {
		g.emitString(0)
	} else {
		g.emitRaw([]byte("\""))
		g.flushRaw()
		g.emitInteger(0, key.Bits()/8, key.Kind() >= reflect.Int && key.Kind() <= reflect.Int64)
		g.emitRaw([]byte("\""))
	}
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
	g.loadMapFrame(_RAX)
	g.emitHelper(jitHelperMapNext)
	// Next is a public reflect method. It preserves the runtime's map growth,
	// iteration order and concurrent-write checks without a legacy shim.
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
