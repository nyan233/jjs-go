package compiler

import (
	"math"
	"reflect"
	"unsafe"
)

const jitDigitPairs = "0001020304050607080910111213141516171819" +
	"2021222324252627282930313233343536373839" +
	"4041424344454647484950515253545556575859" +
	"6061626364656667686970717273747576777879" +
	"8081828384858687888990919293949596979899"

// Reserve is inline on the usual path. Only buffer growth enters Go, where
// allocation and pointer write barriers belong. Seven bytes of padding permit
// the existing literal writer's final eight-byte store.
func (g *jitEncoderGenerator) emitReserve(n int) {
	c := g.code
	slow, done := c.newLabel(), c.newLabel()
	c.loadMem(_R10, 8, _RBX)
	c.loadMem(_R10, 16, _RCX)
	c.lea(_RBX, int64(n+7), _RAX)
	c.compare(_RCX, _RAX)
	c.branch(jitBelow, slow)
	c.loadMem(_R10, 0, _RAX)
	c.add(_RBX, _RAX)
	c.addConst(int64(n), _RBX)
	c.mov(_RBX, mem(_R10, 8))
	c.branch(jitAlways, done)
	c.bind(slow)
	c.mov(_R10, _RAX)
	c.movConst(int64(n), _RBX)
	g.emitHelper(jitHelperReserve)
	c.bind(done)
}

// BX is a bounded, positive reservation size. Used by the inline Base64 path.
func (g *jitEncoderGenerator) emitReserveVariable() {
	c := g.code
	slow, done := c.newLabel(), c.newLabel()
	c.loadMem(_R10, 8, _RCX)
	c.add(_RBX, _RCX)
	c.lea(_RCX, 7, _RAX)
	c.loadMem(_R10, 16, _RDI)
	c.compare(_RDI, _RAX)
	c.branch(jitBelow, slow)
	c.mov(_RCX, mem(_R10, 8))
	c.subtract(_RBX, _RCX)
	c.loadMem(_R10, 0, _RAX)
	c.add(_RCX, _RAX)
	c.branch(jitAlways, done)
	c.bind(slow)
	c.mov(_R10, _RAX)
	g.emitHelper(jitHelperReserve)
	c.bind(done)
}

func (g *jitEncoderGenerator) emitBool(offset int64) {
	c := g.code
	g.emitReserve(5)
	c.loadInteger(_R12, offset, 1, false, _RBX)
	no, done := c.newLabel(), c.newLabel()
	c.compareConst(0, _RBX)
	c.branch(jitEqual, no)
	c.movConst(0x65757274, _RBX) // true
	c.store32(_RBX, mem(_RAX, 0))
	c.loadMem(_R10, 8, _RBX)
	c.subConst(1, _RBX)
	c.mov(_RBX, mem(_R10, 8))
	c.branch(jitAlways, done)
	c.bind(no)
	c.movConst(0x736c6166, _RBX) // fals
	c.store32(_RBX, mem(_RAX, 0))
	c.movConst('e', _RBX)
	c.store8(_RBX, mem(_RAX, 4))
	c.bind(done)
}

func (g *jitEncoderGenerator) emitInteger(offset int64, width int, signed bool) {
	c := g.code
	g.emitReserve(24)
	c.mov(_RAX, _RT1)
	c.loadInteger(_R12, offset, width, signed, _RBX)
	if signed {
		positive := c.newLabel()
		c.compareConst(0, _RBX)
		c.branch(jitNonNegative, positive)
		// Unsigned magnitude also handles MinInt64 without overflow.
		c.negate(_RBX)
		c.movConst('-', _RAX)
		c.store8(_RAX, mem(_RT1, 0))
		c.addConst(1, _RT1)
		c.bind(positive)
	}
	c.lea(_RSP, c.scratchSlot()+24, _RDI)
	loop, last, single, copyDigits := c.newLabel(), c.newLabel(), c.newLabel(), c.newLabel()
	c.bind(loop)
	c.compareConst(100, _RBX)
	c.branch(jitBelow, last)
	c.divide100()
	g.emitDigitPair()
	c.mov(_RCX, _RBX)
	c.branch(jitAlways, loop)
	c.bind(last)
	c.compareConst(10, _RBX)
	c.branch(jitBelow, single)
	c.mov(_RBX, _RSI)
	g.emitDigitPair()
	c.branch(jitAlways, copyDigits)
	c.bind(single)
	c.addConst('0', _RBX)
	c.subConst(1, _RDI)
	c.store8(_RBX, mem(_RDI, 0))
	c.bind(copyDigits)
	c.lea(_RSP, c.scratchSlot()+24, _RCX)
	c.subtract(_RDI, _RCX)
	copyLoop, copyDone := c.newLabel(), c.newLabel()
	c.bind(copyLoop)
	// Scratch and output both include seven padding bytes. Copy in words,
	// then publish only the actual digit count.
	c.loadMem(_RDI, 0, _RAX)
	c.mov(_RAX, mem(_RT1, 0))
	c.compareConst(9, _RCX)
	c.branch(jitBelow, copyDone)
	c.addConst(8, _RT1)
	c.addConst(8, _RDI)
	c.subConst(8, _RCX)
	c.branch(jitAlways, copyLoop)
	c.bind(copyDone)
	c.add(_RCX, _RT1)
	c.loadMem(_R10, 0, _RAX)
	c.subtract(_RAX, _RT1)
	c.mov(_RT1, mem(_R10, 8))
}

// SI contains the pair index, DI the backwards scratch cursor; CX survives.
func (g *jitEncoderGenerator) emitDigitPair() {
	c := g.code
	c.loadMem(_R10, int64(unsafe.Offsetof(jitEncodeContext{}.program)), _R11)
	c.loadMem(_R11, int64(unsafe.Offsetof(jitEncodeProgram{}.digits)), _R11)
	c.add(_RSI, _R11)
	c.add(_RSI, _R11)
	c.loadInteger(_R11, 0, 2, false, _RSI)
	c.subConst(2, _RDI)
	c.store16(_RSI, mem(_RDI, 0))
}

// The JIT performs the type load, float32 conversion, finite/format decisions
// and exponent cleanup. Ryu's correctly rounded conversion stays shared in
// strconv.AppendFloat instead of duplicating a large numeric algorithm per field.
func (g *jitEncoderGenerator) emitFloat(offset int64, width int) {
	c := g.code
	g.emitReserve(32)
	c.loadMem(_R10, 8, _RBX)
	c.subConst(32, _RBX)
	c.mov(_RBX, mem(_R10, 8))
	c.loadFloat(_R12, offset, width)
	c.mask(math.MaxInt64, _RBX)
	nonFinite, exponential, format, done := c.newLabel(), c.newLabel(), c.newLabel(), c.newLabel()
	c.compareConst(int64(math.Float64bits(math.Inf(1))), _RBX)
	c.branch(jitAboveEqual, nonFinite)
	c.movConst('f', _RDI)
	c.compareConst(0, _RBX)
	c.branch(jitEqual, format)
	lower, upper := 1e-6, 1e21
	if width == 4 {
		lower, upper = float64(float32(lower)), float64(float32(upper))
	}
	c.compareConst(int64(math.Float64bits(lower)), _RBX)
	c.branch(jitBelow, exponential)
	c.compareConst(int64(math.Float64bits(upper)), _RBX)
	c.branch(jitBelow, format)
	c.bind(exponential)
	c.movConst('e', _RDI)
	c.bind(format)
	c.loadMem(_R10, 0, _RAX)
	c.loadMem(_R10, 8, _RBX)
	c.loadMem(_R10, 16, _RCX)
	c.movConst(-1, _RSI)
	c.floatBitsArgument(int64(width * 8))
	c.loadMem(_R10, int64(unsafe.Offsetof(jitEncodeContext{}.program)), _RT0)
	c.loadMem(_RT0, jitHelperFloatFormat*8, _RT0)
	c.callFloatFormatter()
	c.restoreJITRegs()
	// The 32-byte reservation guarantees AppendFloat cannot grow the slice;
	// its data pointer is unchanged, so only the scalar length is written.
	c.mov(_RBX, mem(_R10, 8))
	c.compareConst(4, _RBX)
	c.branch(jitBelow, done)
	c.add(_RBX, _RAX)
	c.loadInteger(_RAX, -4, 4, false, _RCX)
	c.mask(0xffffff, _RCX)
	c.compareConst(0x302d65, _RCX) // e-0
	c.branch(jitNotEqual, done)
	c.loadInteger(_RAX, -1, 1, false, _RCX)
	c.store8(_RCX, mem(_RAX, -2))
	c.subConst(1, _RBX)
	c.mov(_RBX, mem(_R10, 8))
	c.branch(jitAlways, done)
	c.bind(nonFinite)
	c.mov(_R10, _RAX)
	c.lea(_R12, offset, _RBX)
	c.movConst(int64(width), _RCX)
	g.emitHelper(jitHelperFloat)
	c.bind(done)
}

func (g *jitEncoderGenerator) canInline(typ reflect.Type) bool {
	return g.depth < jitInlineDepth && g.active[typ] == 0 && inlineTypeCost(typ, 0) <= 48
}

func inlineTypeCost(typ reflect.Type, depth int) int {
	if depth >= jitInlineDepth {
		return 49
	}
	switch typ.Kind() {
	case reflect.Struct:
		cost := 0
		for i := 0; i < typ.NumField(); i++ {
			if typ.Field(i).PkgPath == "" {
				cost += inlineTypeCost(typ.Field(i).Type, depth+1)
			}
			if cost > 48 {
				break
			}
		}
		return cost
	case reflect.Pointer, reflect.Slice, reflect.Array:
		return 1 + inlineTypeCost(typ.Elem(), depth+1)
	default:
		return 1
	}
}

func (g *jitEncoderGenerator) pushRoot() {
	g.code.mov(_R12, mem(_RSP, g.code.rootSlot(g.depth)))
	g.depth++
}

func (g *jitEncoderGenerator) popRoot() {
	c := g.code
	g.depth--
	c.loadMem(_RSP, c.rootSlot(g.depth), _R12)
	c.mov(_R12, mem(_RSP, c.dataSlot()))
	c.movConst(0, _R11)
	c.mov(_R11, mem(_RSP, c.rootSlot(g.depth)))
}

func (g *jitEncoderGenerator) emitPointer(elem reflect.Type, offset int64) error {
	c := g.code
	nilPointer, done := c.newLabel(), c.newLabel()
	c.loadMem(_R12, offset, _RBX)
	c.compareConst(0, _RBX)
	c.branch(jitEqual, nilPointer)
	g.pushRoot()
	c.mov(_RBX, _R12)
	c.mov(_R12, mem(_RSP, c.dataSlot()))
	previous := g.addressable
	g.addressable = true
	err := g.emitValue(elem, 0)
	g.addressable = previous
	if err != nil {
		return err
	}
	g.flushRaw()
	g.popRoot()
	c.branch(jitAlways, done)
	c.bind(nilPointer)
	g.emitRaw([]byte("null"))
	g.flushRaw()
	c.bind(done)
	return nil
}

// Arrays use the same bounded loop as slices when unrolling would grow code.
func (g *jitEncoderGenerator) emitSequence(elem reflect.Type, offset int64, length int) error {
	g.flushRaw()
	c := g.code
	nilSlice, closeArray, done, loop := c.newLabel(), c.newLabel(), c.newLabel(), c.newLabel()
	if length < 0 {
		c.loadMem(_R12, offset, _RBX)
		c.loadMem(_R12, offset+8, _RCX)
		c.compareConst(0, _RBX)
		c.branch(jitEqual, nilSlice)
	} else {
		c.lea(_R12, offset, _RBX)
		c.movConst(int64(length), _RCX)
	}
	countSlot := c.countSlot(g.depth)
	c.mov(_RCX, mem(_RSP, countSlot))
	g.pushRoot()
	c.mov(_RBX, _R12)
	c.mov(_R12, mem(_RSP, c.dataSlot()))
	g.emitRaw([]byte("["))
	g.flushRaw()
	c.loadMem(_RSP, countSlot, _RCX)
	c.compareConst(0, _RCX)
	c.branch(jitEqual, closeArray)
	c.bind(loop)
	previous := g.addressable
	if length < 0 {
		g.addressable = true
	}
	err := g.emitValue(elem, 0)
	g.addressable = previous
	if err != nil {
		return err
	}
	g.flushRaw()
	c.loadMem(_R10, int64(unsafe.Offsetof(jitEncodeContext{}.err)), _RBX)
	c.compareConst(0, _RBX)
	c.branch(jitNotEqual, g.returnLabel)
	c.loadMem(_RSP, countSlot, _RCX)
	c.subConst(1, _RCX)
	c.mov(_RCX, mem(_RSP, countSlot))
	c.compareConst(0, _RCX)
	c.branch(jitEqual, closeArray)
	c.addConst(int64(elem.Size()), _R12)
	c.mov(_R12, mem(_RSP, c.dataSlot()))
	// An entirely inline primitive loop still needs a bounded scheduling point.
	c.mask(16383, _RCX)
	noYield := c.newLabel()
	c.compareConst(0, _RCX)
	c.branch(jitNotEqual, noYield)
	g.emitHelper(jitHelperYield)
	c.bind(noYield)
	g.emitRaw([]byte(","))
	g.flushRaw()
	c.branch(jitAlways, loop)
	c.bind(closeArray)
	g.emitRaw([]byte("]"))
	g.flushRaw()
	g.popRoot()
	c.branch(jitAlways, done)
	c.bind(nilSlice)
	g.emitRaw([]byte("null"))
	g.flushRaw()
	c.bind(done)
	return nil
}
