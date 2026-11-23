package compiler

import (
	"reflect"
	"runtime"
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
