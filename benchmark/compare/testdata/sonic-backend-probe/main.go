// This probe is built in a temporary module under github.com/bytedance/sonic/
// so it can inspect the selected internal backend without modifying Sonic.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"reflect"
	"runtime"
	"time"

	"github.com/bytedance/sonic"
	"github.com/bytedance/sonic/internal/encoder/vars"
	"github.com/bytedance/sonic/internal/rt"
	"github.com/nyan233/jjs-go/benchmark/compare"
)

func run() error {
	size := flag.String("size", "Small", "Small, Medium or Large")
	expectVM := flag.Bool("expect-vm", false, "expected encoder backend")
	flag.Parse()
	if vars.UseVM != *expectVM {
		return fmt.Errorf("UseVM=%v, expected %v", vars.UseVM, *expectVM)
	}
	var value any
	switch *size {
	case "Small":
		value = compare.SmallBook
	case "Medium":
		value = compare.MediumValue
	case "Large":
		value = compare.LargeValue
	default:
		return fmt.Errorf("unknown size %q", *size)
	}
	typ := reflect.TypeOf(value)
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	start := time.Now()
	err := sonic.Pretouch(typ)
	elapsed := time.Since(start)
	runtime.ReadMemStats(&after)
	if err != nil {
		return err
	}
	program := vars.GetProgram(rt.UnpackType(typ))
	if program == nil {
		return fmt.Errorf("no cached encoder for %v", typ)
	}
	var function string
	if v := reflect.ValueOf(program); v.Kind() == reflect.Func {
		if f := runtime.FuncForPC(v.Pointer()); f != nil {
			function = f.Name()
		}
	}
	if !vars.UseVM && function == "" {
		return fmt.Errorf("JIT backend has no registered cached function: %T", program)
	}
	if _, err := sonic.Marshal(value); err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(struct {
		Size, Type, CachedProgram, Function string
		UseVM                               bool
		PretouchNS                          int64
		PretouchBytes, PretouchAllocs       uint64
	}{*size, typ.String(), fmt.Sprintf("%T", program), function, vars.UseVM,
		elapsed.Nanoseconds(), after.TotalAlloc - before.TotalAlloc, after.Mallocs - before.Mallocs})
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
