package compiler

import (
	"reflect"
	"runtime"
	"strconv"
	"testing"
	"unsafe"
)

// Taking the helper arguments' addresses forces Go to use the caller-provided
// spill area, even when this particular call does not need to grow its stack.
//
//go:noinline
func frameTestInteger(ctx *jitEncodeContext, ptr unsafe.Pointer, width, signed int) {
	frameTestSpill(&ctx, &ptr, &width, &signed)
	jitAppendIntPtr(ctx, ptr, width, signed)
}

//go:noinline
func frameTestFloat(dst []byte, value float64, format byte, precision, bits int) []byte {
	frameTestFloatSpill(&dst, &value, &format, &precision, &bits)
	frameTestGrow(32)
	return strconv.AppendFloat(dst, value, format, precision, bits)
}

//go:noinline
func frameTestFloatSpill(dst *[]byte, value *float64, format *byte, precision, bits *int) {
	runtime.KeepAlive(dst)
	runtime.KeepAlive(value)
	runtime.KeepAlive(format)
	runtime.KeepAlive(precision)
	runtime.KeepAlive(bits)
}

func TestJITInlineFloatArgumentSpills(t *testing.T) {
	value := struct {
		Values []float64
		Tail   int
	}{[]float64{1.25, -1e-9, 1e21}, 42}
	compiled, err := compileJITEncoder(reflect.TypeOf(&value))
	if err != nil {
		t.Fatal(err)
	}
	compiled.program.helpers[jitHelperFloatFormat] = reflect.ValueOf(frameTestFloat).Pointer()
	done := make(chan struct{})
	go func() {
		defer close(done)
		got, err := compiled.encode(nil, unsafe.Pointer(&value))
		if err != nil {
			t.Error(err)
			return
		}
		if string(got) != `{"Values":[1.25,-1e-9,1e+21],"Tail":42}` {
			t.Errorf("got %s", got)
		}
	}()
	<-done
}

//go:noinline
func frameTestSpill(ctx **jitEncodeContext, ptr *unsafe.Pointer, width, signed *int) {
	runtime.KeepAlive(ctx)
	runtime.KeepAlive(ptr)
	runtime.KeepAlive(width)
	runtime.KeepAlive(signed)
}

//go:noinline
func frameTestGrow(depth int) {
	var scratch [1024]byte
	scratch[0] = byte(depth)
	if depth == 0 {
		runtime.GC()
	} else {
		frameTestGrow(depth - 1)
	}
	runtime.KeepAlive(scratch)
}

//go:noinline
func frameTestIntegerWithGC(ctx *jitEncodeContext, ptr unsafe.Pointer, width, signed int) {
	frameTestSpill(&ctx, &ptr, &width, &signed)
	frameTestGrow(32)
	jitAppendIntPtr(ctx, ptr, width, signed)
}

func TestJITHelperArgumentSpills(t *testing.T) {
	checkJITFrame(t, frameTestInteger)
}

func TestJITStackGrowthAndGC(t *testing.T) {
	// Run on a fresh goroutine so recursion grows a small initial stack while
	// a generated frame is active. GC must also be able to walk that frame.
	done := make(chan struct{})
	go func() {
		defer close(done)
		checkJITFrame(t, frameTestIntegerWithGC)
	}()
	<-done
}

func checkJITFrame(t *testing.T, helper func(*jitEncodeContext, unsafe.Pointer, int, int)) {
	t.Helper()
	value := struct{ A, B int }{42, 7}
	compiled, err := compileJITEncoder(reflect.TypeOf(value))
	if err != nil {
		t.Fatal(err)
	}
	// Keep an explicit helper call: integer fields now inline and would no
	// longer exercise argument spills merely by replacing this table entry.
	generator := &jitEncoderGenerator{code: newJITCodeBuilder("frame_test"), compiler: &jitEncoderCompiler{}}
	generator.emitPrologue()
	for i, prefix := range []string{`{"A":`, `,"B":`} {
		generator.emitRaw([]byte(prefix))
		generator.flushRaw()
		generator.code.mov(_R10, _RAX)
		generator.code.lea(_R12, int64(i*8), _RBX)
		generator.code.movConst(8, _RCX)
		generator.code.movConst(1, _RDI)
		generator.emitHelper(jitHelperInt)
	}
	generator.emitRaw([]byte("}"))
	generator.emitReturn()
	code, err := generator.code.assemble()
	if err != nil {
		t.Fatal(err)
	}
	function := loadJITFunctionFrame(code, "frame_test", generator.code.pcsp, int(generator.code.frameSize), generator.code.localPointers())
	compiled.function = *(*func(*jitEncodeContext) uintptr)(unsafe.Pointer(&function))
	compiled.program.helpers[jitHelperInt] = reflect.ValueOf(helper).Pointer()
	ctx := &jitEncodeContext{data: unsafe.Pointer(&value), program: compiled.program}
	compiled.function(ctx)
	if ctx.err != nil {
		t.Fatal(ctx.err)
	}
	if got := string(ctx.dst); got != `{"A":42,"B":7}` {
		t.Fatalf("got %s", got)
	}
}
