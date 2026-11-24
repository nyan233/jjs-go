package compiler

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math"
	"math/rand"
	"reflect"
	"runtime"
	"strconv"
	"testing"
)

func unexpectedInlineHelper() { panic("typed fast path called a generic JIT helper") }

func TestJITInlineAvoidsTypeHelpers(t *testing.T) {
	number := 42
	value := struct {
		Bool    bool
		Int     int
		Float   float64
		Pointer *int
		Nil     *int
		Slice   []int
		Bytes   []byte
	}{
		true, -42, 1.25, &number, nil, []int{1, -2, 3}, []byte("hello"),
	}
	program, err := compileJITEncoder(reflect.TypeOf(&value))
	if err != nil {
		t.Fatal(err)
	}
	indices := []int{jitHelperBool, jitHelperInt, jitHelperFloat, jitHelperNull, jitHelperValue, jitHelperSlice}
	if runtime.GOARCH == "arm64" {
		indices = append(indices, jitHelperByteSlice)
	}
	for _, index := range indices {
		program.program.helpers[index] = reflect.ValueOf(unexpectedInlineHelper).Pointer()
	}
	got, err := program.encode(nil, reflect.ValueOf(&value).UnsafePointer())
	if err != nil {
		t.Fatal(err)
	}
	want, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("got %s, want %s", got, want)
	}
}

func checkInlineJSON(t *testing.T, value any) {
	t.Helper()
	program, err := loadCompiledEncoder(reflect.TypeOf(value))
	if err != nil {
		t.Fatal(err)
	}
	want, wantErr := json.Marshal(value)
	for _, capacity := range []int{0, 1, len(want), len(want) + 64} {
		dst := make([]byte, 0, capacity)
		got, err := program.encode(dst, reflect.ValueOf(value).UnsafePointer())
		if (err == nil) != (wantErr == nil) {
			t.Fatalf("%T: got error %v, want %v", value, err, wantErr)
		}
		if err == nil && !bytes.Equal(got, want) {
			i := 0
			for i < min(len(got), len(want)) && got[i] == want[i] {
				i++
			}
			t.Fatalf("%T capacity %d offset %d: got %q, want %q", value, capacity, i, got[max(0, i-24):min(len(got), i+48)], want[max(0, i-24):min(len(want), i+48)])
		}
	}
}

func TestJITInlineIntegers(t *testing.T) {
	for _, value := range []int64{math.MinInt64, math.MaxInt64, math.MinInt32, math.MaxInt32, -10001, -100, -99, -10, -1, 0, 1, 9, 10, 99, 100, 9999, 10000} {
		v := struct {
			I8  int8
			U8  uint8
			I16 int16
			U16 uint16
			I32 int32
			U32 uint32
			I64 int64
			U64 uint64
			I   int
			U   uint
			P   uintptr
			B   bool
		}{
			int8(value), uint8(value), int16(value), uint16(value), int32(value), uint32(value), value, uint64(value), int(value), uint(value), uintptr(value), value%2 == 0,
		}
		checkInlineJSON(t, &v)
	}
	random := rand.New(rand.NewSource(42))
	values := make([]uint64, 10000)
	for i := range values {
		values[i] = random.Uint64()
	}
	for power := uint64(1); power <= math.MaxUint64/10; power *= 10 {
		values = append(values, power-1, power, power+1)
	}
	values = append(values, math.MaxUint64)
	checkInlineJSON(t, &values)
}

func TestJITInlineFloats(t *testing.T) {
	values := []float64{0, math.Copysign(0, -1), 1, -1, 1e-6, 1e-7, 1e20, 1e21, math.SmallestNonzeroFloat64, math.MaxFloat64, math.Inf(1), math.Inf(-1), math.NaN()}
	for _, bound := range []float64{1e-6, 1e21} {
		values = append(values, math.Nextafter(bound, 0), math.Nextafter(bound, math.Inf(1)))
	}
	for _, value := range values {
		checkInlineJSON(t, &value)
		short := float32(value)
		checkInlineJSON(t, &short)
	}
	random := rand.New(rand.NewSource(73))
	for i := 0; i < 2000; i++ {
		v := struct {
			F64 float64
			F32 float32
		}{math.Float64frombits(random.Uint64()), math.Float32frombits(random.Uint32())}
		checkInlineJSON(t, &v)
	}
}

func TestJITInlineContainers(t *testing.T) {
	type leaf struct {
		B     bool
		N     int64
		S     string
		Bytes []byte
	}
	type branch struct {
		Value    *leaf
		List     []leaf
		Pointers []*leaf
		Nested   [][]int
		Empty    []struct{}
	}
	l := leaf{true, math.MinInt64, "text", []byte{0, 255, 1}}
	value := struct {
		A *branch
		B int
		C *leaf
	}{&branch{&l, []leaf{l, {}, l}, []*leaf{nil, &l}, [][]int{nil, {}, {1, 2, 3}}, make([]struct{}, 3)}, 42, nil}
	checkInlineJSON(t, &value)
	value.A.List = nil
	value.A.Pointers = []*leaf{}
	checkInlineJSON(t, &value)
	type node struct {
		N    int
		Next *node
	}
	checkInlineJSON(t, &node{1, &node{2, &node{3, nil}}})
	var array [64]leaf
	array[42] = l
	checkInlineJSON(t, &array)
	var nested [2][17]int
	nested[1][16] = -42
	checkInlineJSON(t, &nested)
	long := make([]int, 40000)
	long[16383], long[16384], long[32768] = -1, 42, math.MaxInt
	checkInlineJSON(t, &long)
}

func TestJITInlineArrayCodeSizeBounded(t *testing.T) {
	// Array length changes the loop bound, not the number of emitted elements.
	small, err := EncoderCodegenStats(reflect.TypeOf([32]int64{}))
	if err != nil {
		t.Fatal(err)
	}
	large, err := EncoderCodegenStats(reflect.TypeOf([32768]int64{}))
	if err != nil {
		t.Fatal(err)
	}
	if large.CodeBytes > small.CodeBytes+32 || large.Functions != small.Functions {
		t.Fatalf("array loop unexpectedly expanded: small=%+v large=%+v", small, large)
	}
}

type inlineGCValue int

func (v *inlineGCValue) MarshalJSON() ([]byte, error) {
	frameTestGrow(32)
	if *v < 0 {
		return nil, errors.New("inline test error")
	}
	return strconv.AppendInt(nil, int64(*v), 10), nil
}

func TestJITInlineNestedStackGrowth(t *testing.T) {
	type leaf struct {
		A    bool
		N    inlineGCValue
		Tail int
	}
	type outer struct {
		P    *leaf
		S    []leaf
		Tail int
	}
	value := outer{&leaf{true, 42, 7}, []leaf{{false, 9, 3}, {true, 11, 5}}, 99}
	done := make(chan struct{})
	go func() { defer close(done); checkInlineJSON(t, &value) }()
	<-done
	value.S[1].N = -1
	checkInlineJSON(t, &value)
}

func TestJITInlineBase64(t *testing.T) {
	var missing []byte
	checkInlineJSON(t, &missing)
	for n := 0; n <= 160; n++ {
		for alignment := 0; alignment < 16; alignment++ {
			data := make([]byte, n+alignment)[alignment:]
			for i := range data {
				data[i] = byte(i*71 + n)
			}
			checkInlineJSON(t, &data)
			want := base64.StdEncoding.EncodeToString(data)
			storage := bytes.Repeat([]byte{0xcc}, len(want)+32)
			encodeBase64(storage[alignment:alignment+len(want)], data)
			if string(storage[alignment:alignment+len(want)]) != want {
				t.Fatalf("kernel n=%d alignment=%d", n, alignment)
			}
			for _, b := range append(storage[:alignment:alignment], storage[alignment+len(want):]...) {
				if b != 0xcc {
					t.Fatalf("kernel wrote outside output n=%d alignment=%d", n, alignment)
				}
			}
		}
	}
	for _, n := range []int{1024, base64ChunkBytes - 1, base64ChunkBytes, base64ChunkBytes + 1, 65536, 1 << 20} {
		data := make([]byte, n)
		rand.New(rand.NewSource(int64(n))).Read(data)
		checkInlineJSON(t, &data)
	}
}

func FuzzJITInlinePrimitives(f *testing.F) {
	f.Add(uint64(0), uint64(0), []byte{})
	f.Add(uint64(math.MaxUint64), math.Float64bits(1e-6), []byte("hello world"))
	f.Fuzz(func(t *testing.T, integer, bits uint64, data []byte) {
		if len(data) > 65536 {
			t.Skip()
		}
		value := struct {
			I    int64
			U    uint64
			F    float64
			B    bool
			Data []byte
		}{int64(integer), integer, math.Float64frombits(bits), integer&1 != 0, data}
		checkInlineJSON(t, &value)
	})
}
