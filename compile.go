package jjs_go

import (
	"github.com/nyan233/jjs-go/internal/compile"
	"github.com/nyan233/jjs-go/internal/rtype"
	"math/rand"
	"reflect"
	"sync"
	"sync/atomic"
	"unsafe"
)

const (
	CComplete int32 = 1 << (10 + iota)
	CHalf
	CInit
)

const (
	marshallerAPI = iota
	unmarshallerAPI
)

var (
	go_GobalJitCompiler = newJitCompiler()
)

type colorNode struct {
	Color     int
	Reference int
	NoObject  bool
	Entry     []colorEntry
}

type colorEntry struct {
	RealType reflect.Type
	SF       reflect.StructField
}

type jitTaskDesc struct {
	TypeDesc reflect.Type
	TaskType int
}

type Compiler interface {
	Compile(typ reflect.Type, isMarshaller bool) *compile.Result
}

type jitCompilerImpl struct {
	mCache  atomic.Pointer[map[uintptr]*compile.Result]
	umCache atomic.Pointer[map[uintptr]*compile.Result]
	mu      sync.Mutex
}

func newJitCompiler() Compiler {
	i := new(jitCompilerImpl)
	i.mCache.Store(&map[uintptr]*compile.Result{})
	i.umCache.Store(&map[uintptr]*compile.Result{})
	return i
}

func (j *jitCompilerImpl) compileMarshaller2(typ reflect.Type) compile.Result {
	stmts := make([]compile.Statement, 0, 0)
	j.compileMarshaller1(typ, &stmts, false)
	p := compile.NewProgram(true, typ)
	p.AppendStmts(stmts)
	return p.Compile()
}

func (j *jitCompilerImpl) compileMarshaller1(typ reflect.Type, stmts *[]compile.Statement, isStruct bool) {
	for _, val := range j.coloration(typ) {
		if val.NoObject {
			*stmts = append(*stmts, compile.Statement{
				OP:     compile.IROutputDynamicString,
				Offset: 0,
				Tail:   nil,
			})
			break
		}
		*stmts = append(*stmts, compile.Statement{
			OP:     compile.IRStartObject,
			Offset: uint64(val.Reference),
			Tail:   nil,
		})
		for k, entry := range val.Entry {
			*stmts = append(*stmts, compile.Statement{
				OP:     compile.IROutputStaticString,
				Offset: 0,
				Tail:   entry.SF.Name,
			})
			*stmts = append(*stmts, compile.Statement{
				OP:     compile.IROutputKeyValSplit,
				Offset: 0,
				Tail:   nil,
			})
			*stmts = append(*stmts, compile.Statement{
				OP:     compile.IROutputDynamicString,
				Offset: uint64(entry.SF.Offset),
				Tail:   entry.RealType,
			})
			if k != len(val.Entry)-1 {
				*stmts = append(*stmts, compile.Statement{
					OP:     compile.IROutputNextSplit,
					Offset: 0,
					Tail:   nil,
				})
			}
		}
		*stmts = append(*stmts, compile.Statement{
			OP:     compile.IREndObject,
			Offset: 0,
			Tail:   nil,
		})
	}
}

func (j *jitCompilerImpl) coloration(typ reflect.Type) []colorNode {
	startr, typ := j.checkReference(typ)
	if !j.mPrepare(typ) {
		return []colorNode{{Color: 1, Reference: int(startr), NoObject: true, Entry: []colorEntry{{RealType: typ}}}}
	}
	color := rand.Int63n(1024 * 1024)
	cc := make([]colorNode, 0, 128)
	j.coloration2(typ, startr, color, &cc, true)
	return cc
}

func (j *jitCompilerImpl) coloration2(typ reflect.Type, startr, color int64, cc *[]colorNode, isStruct bool) {
	if !isStruct {
		(*cc)[len(*cc)-1].Entry = append((*cc)[len(*cc)-1].Entry, colorEntry{RealType: typ})
		return
	}
	if len(*cc) > 0 {
		(*cc)[len(*cc)-1] = colorNode{
			Color:     int(color),
			Reference: int(startr),
			Entry:     nil,
		}
	} else {
		*cc = append(*cc, colorNode{
			Color:     int(color),
			Reference: int(startr),
			Entry:     nil,
		})
	}

	for i := 0; i < typ.NumField(); i++ {
		ft := typ.Field(i)
		switch ft.Type.Kind() {
		case reflect.String:
			(*cc)[len(*cc)-1].Entry = append((*cc)[len(*cc)-1].Entry, colorEntry{
				RealType: ft.Type,
				SF:       ft,
			})
		case reflect.Struct:
			var s2 int64
			var typ2 = ft.Type
			if r, t2 := j.checkReference(ft.Type); r > 0 {
				s2 = startr + r
				typ2 = t2
			}
			j.coloration2(typ2, s2, color+1, cc, true)
		case reflect.Interface:
			break
		case reflect.Map:
			break
		default:
			panic("no support type")
		}
	}
}

func (j *jitCompilerImpl) mPrepare(typ reflect.Type) bool {
	if k := typ.Kind(); k == reflect.Struct || k == reflect.Map {
		return true
	}
	return false
}

func (j *jitCompilerImpl) checkReference(typ reflect.Type) (int64, reflect.Type) {
	var r int64
	for typ.Kind() == reflect.Pointer {
		r++
		typ = typ.Elem()
	}
	return r, typ
}

func (j *jitCompilerImpl) compileUnmarshaller(typ reflect.Type) compile.Result {
	return compile.Result{}
}

func (j *jitCompilerImpl) Compile(typ reflect.Type, isMarshaller bool) *compile.Result {
	emptyFace := (*rtype.EmptyFace)(unsafe.Pointer(&typ))
	result2 := j.loadCache(uintptr(emptyFace.Type), isMarshaller)
	if result2 == nil {
		j.mu.Lock()
		defer j.mu.Unlock()
		result2 = j.loadCache(uintptr(emptyFace.Type), isMarshaller)
		if result2 != nil {
			return result2
		}
		var result compile.Result
		if isMarshaller {
			result = j.compileMarshaller2(typ)
		} else {
			result = j.compileUnmarshaller(typ)
		}
		j.storeCache(uintptr(emptyFace.Type), &result, isMarshaller)
		result2 = &result
	}
	return result2
}
func (j *jitCompilerImpl) loadCache(ptr uintptr, isMCache bool) *compile.Result {
	var instance *map[uintptr]*compile.Result
	if isMCache {
		instance = j.mCache.Load()
	} else {
		instance = j.umCache.Load()
	}
	jc, ok := (*instance)[ptr]
	if !ok {
		return nil
	}
	return jc
}

func (j *jitCompilerImpl) storeCache(ptr uintptr, r *compile.Result, isMCache bool) {
	if j.mu.TryLock() {
		panic("retry lock")
	}
	var instance *map[uintptr]*compile.Result
	var store func(mptr *map[uintptr]*compile.Result)
	if isMCache {
		instance = j.mCache.Load()
		store = func(mptr *map[uintptr]*compile.Result) {
			j.mCache.Store(mptr)
		}
	} else {
		instance = j.umCache.Load()
		store = func(mptr *map[uintptr]*compile.Result) {
			j.umCache.Store(mptr)
		}
	}
	newInstance := make(map[uintptr]*compile.Result)
	for key, value := range *instance {
		newInstance[key] = value
	}
	newInstance[ptr] = r
	store(&newInstance)
}
