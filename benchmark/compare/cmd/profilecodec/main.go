// profilecodec runs one warmed codec workload for an exact operation count.
// It keeps compilation, fixture validation and optional profiler attachment
// outside the measured interval. See PMU_METHODOLOGY_ARM64.md for PMU scope.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"runtime/pprof"
	"time"

	"github.com/bytedance/sonic"
	sonicencoder "github.com/bytedance/sonic/encoder"
	jjs "github.com/nyan233/jjs-go"
	"github.com/nyan233/jjs-go/benchmark/compare"
)

type workload struct {
	run   func() error
	check func() error
	bytes int
	typ   string
}

var compatible = sonic.Config{
	EscapeHTML: true, CompactMarshaler: true, CopyString: true, ValidateString: true,
}.Froze()

func typedWorkload[T any](codec, operation string, input []byte, value T) (workload, error) {
	var marshal func(any) ([]byte, error)
	var marshalTo func([]byte, any) ([]byte, error)
	var unmarshal func([]byte, any) error
	switch codec {
	case "jjs":
		marshal, marshalTo, unmarshal = jjs.Marshal, jjs.MarshalTo, jjs.Unmarshal
	case "sonic":
		marshal, unmarshal = compatible.Marshal, compatible.Unmarshal
		marshalTo = func(dst []byte, value any) ([]byte, error) {
			dst = dst[:0]
			err := sonicencoder.EncodeInto(&dst, value, sonicencoder.EscapeHTML|sonicencoder.CompactMarshaler|sonicencoder.ValidateString)
			return dst, err
		}
		if err := sonic.Pretouch(reflect.TypeFor[T]()); err != nil {
			return workload{}, err
		}
	default:
		return workload{}, fmt.Errorf("unknown codec %q", codec)
	}
	w := workload{bytes: len(input), typ: reflect.TypeFor[T]().String()}
	// Pre-box the same concrete value for both libraries. This isolates codec
	// execution from repeatedly boxing a large value at the call site.
	boxed := any(value)
	var output []byte
	var target T
	switch operation {
	case "encode":
		w.run = func() (err error) { output, err = marshal(boxed); return err }
	case "encode-reuse":
		w.run = func() (err error) { output, err = marshalTo(output, boxed); return err }
	case "decode-reuse":
		w.run = func() error { return unmarshal(input, &target) }
	case "decode-fresh":
		w.run = func() error {
			var fresh T
			if err := unmarshal(input, &fresh); err != nil {
				return err
			}
			target = fresh
			return nil
		}
	default:
		return workload{}, fmt.Errorf("unknown operation %q", operation)
	}
	w.check = func() error {
		if operation == "encode" || operation == "encode-reuse" {
			want, err := json.Marshal(value)
			if err != nil {
				return err
			}
			// Map order is intentionally unsorted for both codecs. UseNumber
			// preserves numeric spellings instead of rounding large integers.
			decode := func(data []byte) (any, error) {
				d := json.NewDecoder(bytes.NewReader(data))
				d.UseNumber()
				var v any
				err := d.Decode(&v)
				return v, err
			}
			gotValue, err := decode(output)
			if err != nil {
				return err
			}
			wantValue, err := decode(want)
			if err != nil {
				return err
			}
			if !reflect.DeepEqual(gotValue, wantValue) {
				return fmt.Errorf("%s output differs from encoding/json", codec)
			}
			return nil
		}
		var want T
		if err := json.Unmarshal(input, &want); err != nil {
			return err
		}
		if !reflect.DeepEqual(target, want) {
			return fmt.Errorf("%s decoded value differs from encoding/json", codec)
		}
		return nil
	}
	return w, nil
}

func selectWorkload(codec, operation, size string) (workload, error) {
	switch size {
	case "Small":
		return typedWorkload(codec, operation, compare.SmallJSON, compare.SmallBook)
	case "Medium":
		return typedWorkload(codec, operation, compare.MediumJSON, compare.MediumValue)
	case "Large":
		return typedWorkload(codec, operation, compare.LargeJSON, compare.LargeValue)
	default:
		return workload{}, fmt.Errorf("unknown size %q", size)
	}
}

func heapProfile(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	err = pprof.WriteHeapProfile(f)
	return errors.Join(err, f.Close())
}

func counterGate(marker string) error {
	fmt.Fprintln(os.Stderr, marker)
	var signal [1]byte
	_, err := io.ReadFull(os.Stdin, signal[:])
	return err
}

func run() (runErr error) {
	codec := flag.String("codec", "jjs", "jjs or sonic")
	operation := flag.String("operation", "encode-reuse", "encode, encode-reuse, decode-reuse or decode-fresh")
	size := flag.String("size", "Large", "Small, Medium or Large")
	n := flag.Int("n", 10000, "exact measured operation count")
	delay := flag.Duration("delay", 0, "wait after warmup for an external profiler to attach")
	gate := flag.Bool("counter-gate", false, "wait for one stdin byte before and after the measured loop")
	pmuGroup := flag.String("pmu", "", "optional privileged worker counters: branch, cache, tlb or simd")
	workerQoS := flag.String("worker-qos", "", "optional macOS worker QoS: user-initiated (process lifetime)")
	profiles := flag.String("profiles", "", "directory for CPU and before/after heap profiles")
	collectCPU := flag.Bool("cpu", true, "collect CPU samples when profiles is set; disable for allocation-only captures")
	memRate := flag.Int("mem-rate", 512*1024, "heap profile sampling interval; 1 records every allocation")
	flag.Parse()
	if *n <= 0 || *memRate <= 0 {
		return fmt.Errorf("n and mem-rate must be positive")
	}
	runtime.MemProfileRate = *memRate
	if *pmuGroup != "" || *workerQoS != "" {
		runtime.LockOSThread()
		// A thread with an unspecified initial QoS cannot restore that class
		// through the public setter. Keep this CLI worker locked until process
		// exit when QoS is requested; never return it to the runtime's pool.
		if *workerQoS == "" {
			defer runtime.UnlockOSThread()
		}
	}
	var qos map[string]int
	if *workerQoS != "" {
		var err error
		qos, err = requestWorkerQoS(*workerQoS)
		if err != nil {
			return err
		}
	}
	w, err := selectWorkload(*codec, *operation, *size)
	if err != nil {
		return err
	}
	for range 20 {
		if err := w.run(); err != nil {
			return err
		}
	}
	if err := w.check(); err != nil {
		return err
	}
	runtime.GC()
	var cpu *os.File
	if *profiles != "" {
		if err := os.MkdirAll(*profiles, 0755); err != nil {
			return err
		}
		if err := heapProfile(filepath.Join(*profiles, "before.heap.pprof")); err != nil {
			return err
		}
		if *collectCPU {
			cpu, err = os.Create(filepath.Join(*profiles, "cpu.pprof"))
			if err != nil {
				return err
			}
			defer cpu.Close()
		}
	}
	fmt.Fprintf(os.Stderr, "ready pid=%d codec=%s operation=%s size=%s n=%d\n", os.Getpid(), *codec, *operation, *size, *n)
	time.Sleep(*delay)
	var pmu *pmuSession
	if *pmuGroup != "" {
		pmu, err = openPMU(*pmuGroup)
		if err != nil {
			return err
		}
		defer func() { runErr = errors.Join(runErr, pmu.close()) }()
	}
	if cpu != nil {
		if err := pprof.StartCPUProfile(cpu); err != nil {
			return err
		}
	}
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	if *gate {
		if err := counterGate("counter-ready"); err != nil {
			return err
		}
	}
	var pmuBefore, pmuAfter [5]uint64
	if pmu != nil {
		pmuBefore, err = pmu.sample()
		if err != nil {
			return err
		}
	}
	start := time.Now()
	for range *n {
		if err := w.run(); err != nil {
			return err
		}
	}
	end := time.Now()
	if pmu != nil {
		pmuAfter, err = pmu.sample()
		if err != nil {
			return err
		}
	}
	runtime.ReadMemStats(&after)
	if *gate {
		if err := counterGate("counter-done"); err != nil {
			return err
		}
	}
	if cpu != nil {
		pprof.StopCPUProfile()
	}
	if *profiles != "" {
		runtime.GC()
		if err := heapProfile(filepath.Join(*profiles, "after.heap.pprof")); err != nil {
			return err
		}
	}
	if err := w.check(); err != nil {
		return err
	}
	pmuDelta := make(map[string]uint64)
	pmuStart := make(map[string]uint64)
	pmuEnd := make(map[string]uint64)
	var pmuWrapped []string
	if pmu != nil {
		for i, name := range pmu.names {
			if pmuAfter[i] < pmuBefore[i] {
				// XNU accumulates virtual thread counters modulo uint64. A
				// reconfigured counter can start at a negative virtual offset.
				if pmuBefore[i] < 1<<63 {
					return fmt.Errorf("PMU reset in %s: before=%v after=%v", name, pmuBefore, pmuAfter)
				}
				pmuWrapped = append(pmuWrapped, name)
			}
			pmuDelta[name] = pmuAfter[i] - pmuBefore[i]
			pmuStart[name], pmuEnd[name] = pmuBefore[i], pmuAfter[i]
		}
		fixed := pmuDelta["fixed_instructions"]
		if pmuDelta["fixed_cycles"] == 0 || fixed == 0 || (*pmuGroup != "simd" && pmuDelta["user_instructions"] == 0) {
			return fmt.Errorf("PMU returned zero cycles or instructions")
		}
		if pmuDelta["user_instructions"] > fixed+fixed/100+4096 {
			return fmt.Errorf("PMU user instructions exceed fixed total: %v", pmuDelta)
		}
		for _, name := range pmu.names[2:] {
			if pmuDelta[name] > fixed*1024 {
				return fmt.Errorf("implausible PMU event delta %s: %v", name, pmuDelta)
			}
		}
	}
	result := struct {
		PMUBefore, PMUAfter                     map[string]uint64
		PMUWrapped                              []string
		Codec, Operation, Size, Type, GoVersion string
		PID, GOMAXPROCS, N, InputBytes          int
		StartUnixNano, EndUnixNano              int64
		NSPerOp, BytesPerOp, AllocsPerOp        float64
		GCs, PauseNS                            uint64
		PMUGroup                                string
		PMU                                     map[string]uint64
		WorkerQoS                               map[string]int
	}{
		pmuStart, pmuEnd, pmuWrapped,
		*codec, *operation, *size, w.typ, runtime.Version(),
		os.Getpid(), runtime.GOMAXPROCS(0), *n, w.bytes,
		start.UnixNano(), end.UnixNano(),
		float64(end.Sub(start).Nanoseconds()) / float64(*n),
		float64(after.TotalAlloc-before.TotalAlloc) / float64(*n),
		float64(after.Mallocs-before.Mallocs) / float64(*n),
		uint64(after.NumGC - before.NumGC), after.PauseTotalNs - before.PauseTotalNs,
		*pmuGroup, pmuDelta,
		qos,
	}
	return json.NewEncoder(os.Stdout).Encode(result)
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
