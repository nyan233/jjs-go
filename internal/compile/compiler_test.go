package compile

import (
	"encoding/hex"
	"fmt"
	"github.com/nyan233/jjs-go/internal/asm"
	"github.com/nyan233/jjs-go/internal/mempool"
	"reflect"
	"runtime"
	"runtime/debug"
	"testing"
	"unsafe"
)

var (
	testIR = []Statement{
		{OP: IRStartObject},
		{OP: IROutputStaticString, Tail: "UserName"},
		{OP: IROutputKeyValSplit},
		{OP: IROutputDynamicString, Offset: 0, Tail: reflect.TypeOf(*new(string))},
		{OP: IROutputNextSplit},
		{OP: IROutputStaticString, Tail: "UserId"},
		{OP: IROutputKeyValSplit},
		{OP: IROutputDynamicString, Offset: 16, Tail: reflect.TypeOf(*new(string))},
		{OP: IREndObject},
	}
)

type TestJsonTyp struct {
	UserName string
	UserId   string
}

func TestCompile(t *testing.T) {
	debug.SetGCPercent(-1)
	p := NewProgram(true, nil)
	p.AppendStmts(testIR)
	result := p.Compile()
	td := &TestJsonTyp{
		UserName: "hello-person2222333937429hf92qhf9qhf9q238f",
		UserId:   "wuuuwuhello2333f32q89492374",
	}
	t.Log(len(result.Text))
	t.Log(hex.EncodeToString(result.Text))
	dest := make([]byte, 1024, 1024)
	stack := mempool.StackPool.Get().(*mempool.Stack)
	t.Log(fmt.Sprintf("0x%x", uintptr(unsafe.Pointer(&dest[0]))))
	t.Log(fmt.Sprintf("0x%x", unsafe.Pointer(td)))
	write, _ := asm.CallMFunc(result.MFunc, uintptr(stack.High), unsafe.Pointer(&dest[0]), unsafe.Pointer(td))
	dest = dest[:write+result.MinSize]
	t.Log(string(dest))
	runtime.KeepAlive(dest)
	runtime.KeepAlive(stack)
	runtime.KeepAlive(td)
}

func TestLibrary(t *testing.T) {
	p := newProgram()
	link, _ := p.AppendP(nil)
	buildCallMemoryCopy(p, link)
	t.Log(hex.EncodeToString(p.BuildOnFunc(1)))
}

func TestUtils(t *testing.T) {
	t.Run("Bytes2Int64", func(t *testing.T) {
		var (
			bytes    = [8]byte{0x12, 0x34, 0x56, 0x78, 0x78, 0x56, 0x34, 0x12}
			bytesHex = "0x1234567878563412"
		)
		s := fmt.Sprintf("0x%x", bytes2Int64(bytes[:]))
		if s != bytesHex {
			t.Error("result error : ", s)
		}
	})
	t.Run("GetFuncAddress", func(t *testing.T) {
		ptr := getFunctionCodeAddress(getFuncAddrByName, false)
		fn := *(*func(name string) funcDesc)(unsafe.Pointer(&ptr))
		desc := fn("Hello-World")
		_ = desc
	})
}
