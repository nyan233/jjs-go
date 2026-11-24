package compiler

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"unsafe"
)

func decodeMapJSON(t *testing.T, data []byte) any {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		t.Fatal(err)
	}
	return value
}

func checkMapJSON(t *testing.T, value any) {
	t.Helper()
	program, err := loadCompiledEncoder(reflect.TypeOf(value))
	if err != nil {
		t.Fatal(err)
	}
	want, wantErr := json.Marshal(value)
	for _, capacity := range []int{0, 1, len(want), len(want) + 128} {
		got, err := program.encode(make([]byte, 0, capacity), reflect.ValueOf(value).UnsafePointer())
		if (err == nil) != (wantErr == nil) {
			t.Fatalf("%T: error %v, want %v", value, err, wantErr)
		}
		if err != nil {
			continue
		}
		if !json.Valid(got) {
			t.Fatalf("%T: invalid JSON %.200q", value, got)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("%T capacity=%d: got %.300s, want %.300s", value, capacity, got, want)
		}
	}
}

func checkMapDecode(t *testing.T, source any) {
	t.Helper()
	data, err := json.Marshal(source)
	if err != nil {
		t.Fatal(err)
	}
	typ := reflect.TypeOf(source)
	decoder, err := CompileDecoder(typ)
	if err != nil {
		t.Fatal(err)
	}
	got := reflect.New(typ)
	if err := decoder(data, unsafe.Pointer(got.Pointer())); err != nil {
		t.Fatalf("%T: %v", source, err)
	}
	want := reflect.New(typ)
	if err := json.Unmarshal(data, want.Interface()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Interface(), want.Interface()) {
		t.Fatalf("%T: got %#v, want %#v", source, got.Interface(), want.Interface())
	}
}

// Go only promises the low byte of a bool return register. Deliberately leave
// the upper bits nonzero even when iteration finishes, as amd64 SETNE can do.
func mapTestNextWide(ctx *jitEncodeContext) uint64 {
	if jitMapNext(ctx) {
		return 0x12340001
	}
	return 0x12340000
}

func TestJITMapBoolReturnABI(t *testing.T) {
	value := map[string]int{"entry": 42}
	program, err := compileJITEncoder(reflect.TypeOf(value))
	if err != nil {
		t.Fatal(err)
	}
	program.program.helpers[jitHelperMapNext] = reflect.ValueOf(mapTestNextWide).Pointer()
	got, err := program.encode(nil, reflect.ValueOf(value).UnsafePointer())
	if err != nil || string(got) != `{"entry":42}` {
		t.Fatalf("got %s, error %v", got, err)
	}
}

type mapJSONKey int

func (mapJSONKey) MarshalJSON() ([]byte, error) {
	return nil, errors.New("map key must not call MarshalJSON")
}

type mapTextKey struct {
	Value string
}

func (key mapTextKey) MarshalText() ([]byte, error) {
	return []byte(key.Value), nil
}

func (key *mapTextKey) UnmarshalText(text []byte) error {
	if string(text) == "bad" {
		return errors.New("bad text map key")
	}
	key.Value = string(text)
	return nil
}

type mapIntTextKey int

func (key mapIntTextKey) MarshalText() ([]byte, error) {
	return strconv.AppendInt([]byte("x"), int64(key), 10), nil
}

func (key *mapIntTextKey) UnmarshalText(text []byte) error {
	value, err := strconv.ParseInt(string(text[1:]), 10, 64)
	if err != nil {
		return err
	}
	*key = mapIntTextKey(value)
	return nil
}

type mapStringTextKey string

func (key mapStringTextKey) MarshalText() ([]byte, error) {
	return []byte("wrong-" + string(key)), nil
}

func (key *mapStringTextKey) UnmarshalText(text []byte) error {
	*key = mapStringTextKey("decoded-" + string(text))
	return nil
}

type mapErrorTextKey int

func (mapErrorTextKey) MarshalText() ([]byte, error) {
	return nil, errors.New("map key marshal error")
}

type mapMutatingTextKey int

var mapMutatingKeyCallback func()

func (key mapMutatingTextKey) MarshalText() ([]byte, error) {
	if mapMutatingKeyCallback != nil {
		callback := mapMutatingKeyCallback
		mapMutatingKeyCallback = nil
		callback()
	}
	return strconv.AppendInt(nil, int64(key), 10), nil
}

type mapCustomValue map[string]int

func (value mapCustomValue) MarshalJSON() ([]byte, error) {
	return strconv.AppendInt(nil, int64(len(value)), 10), nil
}

func TestJITMapCustomValue(t *testing.T) {
	value := mapCustomValue{"one": 1, "two": 2}
	checkMapJSON(t, value)
	checkMapJSON(t, &value)
	checkMapJSON(t, map[string]mapCustomValue{"value": value, "nil": nil})
	checkMapJSON(t, map[string]any{"value": value})
	checkMapJSON(t, mapCustomValue(nil))
	checkMapJSON(t, map[string]any{"nil": mapCustomValue(nil)})
}

func TestJITMapKeyTypes(t *testing.T) {
	for _, typ := range []reflect.Type{
		reflect.TypeOf(int(0)), reflect.TypeOf(int8(0)), reflect.TypeOf(int16(0)), reflect.TypeOf(int32(0)), reflect.TypeOf(int64(0)),
		reflect.TypeOf(uint(0)), reflect.TypeOf(uint8(0)), reflect.TypeOf(uint16(0)), reflect.TypeOf(uint32(0)), reflect.TypeOf(uint64(0)), reflect.TypeOf(uintptr(0)), reflect.TypeOf(mapJSONKey(0)),
	} {
		v := reflect.MakeMap(reflect.MapOf(typ, reflect.TypeOf(false)))
		for _, bits := range []uint64{0, 1, 9, 10, 99, 100, math.MaxUint64, math.MaxInt64, uint64(1) << 63} {
			key := reflect.New(typ).Elem()
			if typ.Kind() >= reflect.Int && typ.Kind() <= reflect.Int64 {
				key.SetInt(int64(bits))
			} else {
				key.SetUint(bits)
			}
			v.SetMapIndex(key, reflect.ValueOf(bits&1 == 0))
		}
		checkMapJSON(t, v.Interface())
		checkMapDecode(t, v.Interface())
	}
	checkMapJSON(t, map[string]string{"": "empty", "中😀": "中文", "\x00\n\t\"\\<>&\u2028\u2029": "escaped", "bad\xff": "invalid UTF-8"})
}

func TestJITMapKeyOrderingAndTextCodec(t *testing.T) {
	pointerKey := &mapTextKey{Value: "a"}
	for _, value := range []any{
		map[int]int{2: 2, 10: 10, -1: -1},
		map[string]int{"10": 10, "2": 2, "": 0},
		map[mapTextKey]int{{Value: "z"}: 1, {Value: "a"}: 2},
		map[mapIntTextKey]int{2: 2, 10: 10},
		map[mapStringTextKey]int{"actual": 1},
		map[*mapTextKey]int{nil: 1, pointerKey: 2},
	} {
		checkMapJSON(t, value)
		// encoding/json can encode pointer keys through TextMarshaler, but its
		// decoder cannot allocate the pointed-to TextUnmarshaler key.
		if reflect.TypeOf(value).Key().Kind() != reflect.Pointer {
			checkMapDecode(t, value)
		}
	}
}

func TestJITMapTextKeyErrors(t *testing.T) {
	checkMapJSON(t, map[mapErrorTextKey]int{1: 1})

	decoder, err := CompileDecoder(reflect.TypeOf(map[mapTextKey]int{}))
	if err != nil {
		t.Fatal(err)
	}
	var got map[mapTextKey]int
	standard, standardErr := func() (map[mapTextKey]int, error) {
		var value map[mapTextKey]int
		err := json.Unmarshal([]byte(`{"bad":1}`), &value)
		return value, err
	}()
	err = decoder([]byte(`{"bad":1}`), unsafe.Pointer(&got))
	if err == nil || standardErr == nil {
		t.Fatalf("got error %v, standard error %v", err, standardErr)
	}
	if !reflect.DeepEqual(got, standard) {
		t.Fatalf("got %#v, want %#v", got, standard)
	}

	for _, test := range []struct {
		data   string
		target any
	}{
		{`{"128":1}`, new(map[int8]int)},
		{`{"-1":1}`, new(map[uint]int)},
		{`{"not-int":1}`, new(map[int64]int)},
	} {
		typ := reflect.TypeOf(test.target).Elem()
		decoder, err := CompileDecoder(typ)
		if err != nil {
			t.Fatal(err)
		}
		standard := reflect.New(typ)
		standardErr := json.Unmarshal([]byte(test.data), standard.Interface())
		got := reflect.New(typ)
		err = decoder([]byte(test.data), unsafe.Pointer(got.Pointer()))
		if err == nil || standardErr == nil {
			t.Fatalf("%s: got error %v, standard error %v", test.data, err, standardErr)
		}
	}
}

func TestJITMapUnsupportedKeyTypes(t *testing.T) {
	for _, value := range []any{map[bool]int{true: 1}, map[float64]int{1: 1}} {
		if _, err := loadCompiledEncoder(reflect.TypeOf(value)); err == nil {
			t.Fatalf("%T: encoder accepted unsupported key", value)
		}
		if _, err := CompileDecoder(reflect.TypeOf(value)); err == nil {
			t.Fatalf("%T: decoder accepted unsupported key", value)
		}
		if _, err := json.Marshal(value); err == nil {
			t.Fatalf("%T: standard encoder unexpectedly accepted key", value)
		}
	}
}

func TestJITMapValues(t *testing.T) {
	for _, test := range mapBenchmarkValues() {
		t.Run(test.name, func(t *testing.T) { checkMapJSON(t, test.value) })
	}
	value := map[string]any{"nil": nil, "bool": true, "string": "text", "integer": int64(math.MinInt64), "float": 1e-9, "slice": []any{1, "s", false}, "map": map[string]int{"k": 2}}
	checkMapJSON(t, value)
	checkMapJSON(t, &value)
	checkMapJSON(t, &struct {
		Before int
		Map    *map[string]any
		After  string
	}{7, &value, "tail"})
	checkMapJSON(t, map[string]*map[string]any{"pointer": &value, "nil": nil})
	checkMapJSON(t, map[string]any{"pointer": &value})
	checkMapJSON(t, map[string]any{"wrapper": struct{ Map map[string]int }{map[string]int{"value": 42}}})
	checkMapJSON(t, map[string]any{"wrapper": [1]map[string]int{{"value": 42}}})
	checkMapJSON(t, map[string]struct{}{"a": {}, "b": {}})
	checkMapJSON(t, map[string][]byte{"nil": nil, "empty": {}, "data": {0, 1, 2, 255}})
	checkMapJSON(t, map[string][129]byte{"large": {1, 2, 3, 128: 255}})
	checkMapJSON(t, map[string]float64{"valid": 1, "invalid": math.NaN()})
	long := strings.Repeat("中😀\n\"<", 12000)
	checkMapJSON(t, map[string]string{long: long})
	checkMapJSON(t, &struct {
		A map[string]int
		B []map[string]int
		C int
	}{map[string]int{"a": 1}, []map[string]int{nil, {}, {"b": 2}}, 42})
	type recursive map[string]recursive
	checkMapJSON(t, recursive{"one": {"two": {"three": {"four": {"five": {}}}}}})
	var deep any = map[string]int{"value": 42}
	for i := 0; i < 12; i++ {
		deep = map[string]any{"child": deep}
	}
	checkMapJSON(t, deep)
	checkMapJSON(t, map[string]inlineGCValue{"unaddressable": 7})
}

func TestJITMapInlineAvoidsValueHelpers(t *testing.T) {
	for _, value := range []any{map[string]string{"a": "text"}, map[int64]uint64{-42: math.MaxUint64}, map[string]map[string]int{"a": {"b": 3}}} {
		program, err := compileJITEncoder(reflect.TypeOf(value))
		if err != nil {
			t.Fatal(err)
		}
		indices := []int{jitHelperValue, jitHelperInt, jitHelperBool}
		if runtime.GOARCH == "arm64" {
			indices = append(indices, jitHelperString)
		}
		for _, index := range indices {
			program.program.helpers[index] = reflect.ValueOf(unexpectedInlineHelper).Pointer()
		}
		got, err := program.encode(nil, reflect.ValueOf(value).UnsafePointer())
		if err != nil || !json.Valid(got) {
			t.Fatalf("%T: %s, %v", value, got, err)
		}
	}
}

func TestJITMapWarmAllocations(t *testing.T) {
	value := map[string]map[string]int{"a": {"x": 1, "y": 2}, "b": {"z": 3}}
	program, err := compileJITEncoder(reflect.TypeOf(value))
	if err != nil {
		t.Fatal(err)
	}
	data := reflect.ValueOf(value).UnsafePointer()
	dst, err := program.encode(nil, data)
	if err != nil {
		t.Fatal(err)
	}
	allocs := testing.AllocsPerRun(100, func() {
		dst, err = program.encode(dst, data)
		if err != nil {
			panic(err)
		}
	})
	if allocs != 0 {
		t.Fatalf("warm nested-map encoding allocates %g objects/op", allocs)
	}
}

type mapTestCallback struct {
	ID  int
	Run func()
}

func (v mapTestCallback) MarshalJSON() ([]byte, error) {
	if v.Run != nil {
		v.Run()
	}
	if v.ID < 0 {
		return nil, errors.New("map callback error")
	}
	return strconv.AppendInt(nil, int64(v.ID), 10), nil
}

var mapTestContexts = make(chan *jitEncodeContext, 1)

//go:noinline
func mapTestCaptureStart(ctx *jitEncodeContext, ptr unsafe.Pointer, index int) {
	jitMapStart(ctx, ptr, index)
	select {
	case mapTestContexts <- ctx:
	default:
	}
}

func TestJITMapCleanupAndStackGrowth(t *testing.T) {
	value := map[string]map[string]mapTestCallback{"nested": {"entry": {ID: 42, Run: func() { frameTestGrow(32) }}}}
	program, err := compileJITEncoder(reflect.TypeOf(value))
	if err != nil {
		t.Fatal(err)
	}
	program.program.helpers[jitHelperMapStart] = reflect.ValueOf(mapTestCaptureStart).Pointer()
	data := reflect.ValueOf(value).UnsafePointer()
	for _, mode := range []string{"success", "error", "panic"} {
		if mode == "error" {
			value["nested"]["entry"] = mapTestCallback{ID: -1, Run: func() { frameTestGrow(32) }}
		}
		if mode == "panic" {
			value["nested"]["entry"] = mapTestCallback{Run: func() { frameTestGrow(32); panic("map test") }}
		}
		done := make(chan struct{})
		go func() {
			defer close(done)
			defer func() {
				if r := recover(); (r != nil) != (mode == "panic") {
					t.Errorf("%s: panic %v", mode, r)
				}
			}()
			_, err := program.encode(nil, data)
			if (err != nil) != (mode == "error") {
				t.Errorf("%s: error %v", mode, err)
			}
		}()
		<-done
		var ctx *jitEncodeContext
		select {
		case ctx = <-mapTestContexts:
		default:
			t.Fatal("map start not reached")
		}
		if ctx.mapActive != nil || ctx.data != nil || ctx.program != nil || ctx.dst != nil || ctx.err != nil {
			t.Fatalf("%s retained context state", mode)
		}
		count := 0
		for frame := ctx.mapFrames; frame != nil; frame = frame.next {
			count++
			if !reflect.DeepEqual(frame.iterator, reflect.MapIter{}) ||
				len(frame.entries) != 0 || frame.valueCount != 0 ||
				frame.name != "" || frame.elem != nil {
				t.Fatalf("%s retained map references", mode)
			}
		}
		if count < 2 {
			t.Fatalf("only %d map frames", count)
		}
	}
}

func TestJITMapGrowthDuringCallback(t *testing.T) {
	value := make(map[string]mapTestCallback)
	for i := 0; i < 64; i++ {
		value[fmt.Sprint(i)] = mapTestCallback{ID: i}
	}
	called := false
	value["trigger"] = mapTestCallback{ID: 100, Run: func() {
		if called {
			t.Fatal("entry visited twice")
		}
		called = true
		for i := 0; i < 64; i++ {
			delete(value, fmt.Sprint(i))
		}
		for i := 0; i < 256; i++ {
			value[fmt.Sprintf("new%d", i)] = mapTestCallback{ID: i}
		}
		runtime.GC()
	}}
	program, err := compileJITEncoder(reflect.TypeOf(value))
	if err != nil {
		t.Fatal(err)
	}
	got, err := program.encode(nil, reflect.ValueOf(value).UnsafePointer())
	if err != nil {
		t.Fatal(err)
	}
	if !called || !json.Valid(got) {
		t.Fatalf("callback=%v invalid JSON %.100s", called, got)
	}
	decoder := json.NewDecoder(bytes.NewReader(got))
	if _, err := decoder.Token(); err != nil {
		t.Fatal(err)
	}
	seen := make(map[string]bool)
	for decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			t.Fatal(err)
		}
		name := key.(string)
		if seen[name] {
			t.Fatalf("duplicate key %s", name)
		}
		seen[name] = true
		var number int
		if err := decoder.Decode(&number); err != nil {
			t.Fatal(err)
		}
	}
}

func TestJITMapGrowthDuringTextKeyCallback(t *testing.T) {
	value := make(map[mapMutatingTextKey]int)
	for i := 0; i < 64; i++ {
		value[mapMutatingTextKey(i)] = i
	}
	called := false
	mapMutatingKeyCallback = func() {
		called = true
		for i := 0; i < 32; i++ {
			delete(value, mapMutatingTextKey(i))
		}
		for i := 0; i < 128; i++ {
			value[mapMutatingTextKey(1000+i)] = i
		}
		runtime.GC()
	}
	defer func() { mapMutatingKeyCallback = nil }()
	program, err := loadCompiledEncoder(reflect.TypeOf(value))
	if err != nil {
		t.Fatal(err)
	}
	got, err := program.encode(nil, reflect.ValueOf(value).UnsafePointer())
	if err != nil {
		t.Fatal(err)
	}
	if !called || !json.Valid(got) {
		t.Fatalf("callback=%v invalid JSON %.100s", called, got)
	}
	decoder := json.NewDecoder(bytes.NewReader(got))
	if _, err := decoder.Token(); err != nil {
		t.Fatal(err)
	}
	seen := make(map[string]bool)
	for decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			t.Fatal(err)
		}
		name := key.(string)
		if seen[name] {
			t.Fatalf("duplicate key %s", name)
		}
		seen[name] = true
		var number int
		if err := decoder.Decode(&number); err != nil {
			t.Fatal(err)
		}
	}
}

func FuzzJITMap(f *testing.F) {
	f.Add("key", "value", int64(-42), []byte("abc"))
	f.Add("\x00中\xff", "\n\"", int64(math.MinInt64), []byte{})
	f.Fuzz(func(t *testing.T, key, value string, number int64, data []byte) {
		if len(key)+len(value)+len(data) > 65536 {
			t.Skip()
		}
		checkMapJSON(t, map[string]any{key: []any{value, number, data, map[string]int64{"nested": number}}})
	})
}
