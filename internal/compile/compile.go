package compile

import (
	"fmt"
	"github.com/bytedance/sonic/loader"
	"github.com/nyan233/jjs-go/internal/mempool"
	"github.com/twitchyliquid64/golang-asm/obj"
	"github.com/twitchyliquid64/golang-asm/obj/x86"
	"reflect"
	"unsafe"
)

var jitLoader = loader.Loader{
	Name: "jjs.jit.",
	File: "github.com/nyan233/jjs/jit.go",
	Options: loader.Options{
		NoPreempt: true,
	},
}

type Result struct {
	StackSize int64
	MinSize   int64
	MFunc     uintptr
	Text      []byte
	UFunc     uintptr
}

type Program struct {
	Arch     string
	IsMarsha bool
	Stmts    *Statement
	goType   reflect.Type
}

type Statement struct {
	OP     uint64
	Offset uint64
	Tail   interface{}
	next   *Statement
}

func NewProgram(isMarshal bool, typ reflect.Type) *Program {
	p := &Program{
		Arch:     "amd64",
		IsMarsha: isMarshal,
		goType:   typ,
	}
	return p
}

func (p *Program) AppendStmts(stmts []Statement) {
	var root *Statement
	if p.Stmts == nil {
		p.Stmts = new(Statement)
		root = p.Stmts
		defer func() {
			p.Stmts = p.Stmts.next
		}()
	}
	for _, stmt := range stmts {
		stmt2 := stmt
		root.next = &stmt2
		root = root.next
	}
}

func (p *Program) Compile() Result {
	if p.IsMarsha {
		return p.compileMarshal()
	}
	return Result{}
}

// Marshall动态字符串的长度计数保存在DX中, 而静态字符串保存为Const, 为目标写入位置的寻址方式为(rax,rdx,1,$const)
// 以上表达式的含义为(base,index,scale,offset)
func (p *Program) compileMarshal() Result {
	asmBuilder := newProgram()
	objl, _ := asmBuilder.AppendP(nil)
	stmt := p.Stmts
	var bytesBaseLen int
	for stmt != nil {
		switch stmt.OP {
		case IRStartObject:
			objl = p.outputSingleByteIR(objl, asmBuilder, &bytesBaseLen, '{')
		case IROutputStaticString:
			// 合并静态Key和分割符的字符串输出
			if stmt.next != nil && stmt.next.OP == IROutputKeyValSplit {
				objl = p.outputStringIR(objl, asmBuilder, &bytesBaseLen, fmt.Sprintf("\"%s\" : ", stmt.Tail.(string)))
				stmt = stmt.next
			} else {
				objl = p.outputStringIR(objl, asmBuilder, &bytesBaseLen, fmt.Sprintf("\"%s\"", stmt.Tail.(string)))
			}
		case IROutputDynamicString:
			objl = p.outputSingleByteIR(objl, asmBuilder, &bytesBaseLen, '"')
			objl = p.outputDynamicStringIR(objl, asmBuilder, int(stmt.Offset), &bytesBaseLen, stmt.Tail.(reflect.Type))
			objl = p.outputSingleByteIR(objl, asmBuilder, &bytesBaseLen, '"')
		case IROutputKeyValSplit:
			objl = p.outputSingleByteIR(objl, asmBuilder, &bytesBaseLen, ':')
		case IROutputNextSplit:
			objl = p.outputSingleByteIR(objl, asmBuilder, &bytesBaseLen, ',')
		case IREndObject:
			objl = p.outputSingleByteIR(objl, asmBuilder, &bytesBaseLen, '}')
		}
		stmt = stmt.next
	}
	codeText := asmBuilder.BuildOnFunc(1)
	text := mempool.GlobalArena.AllocJitCodeMemory(codeText)
	r := Result{
		MFunc: text,
		Text: *(*[]byte)(unsafe.Pointer(&reflect.SliceHeader{
			Data: *(*uintptr)(unsafe.Pointer(text)),
			Len:  len(codeText),
			Cap:  len(codeText),
		})),
		MinSize: int64(bytesBaseLen),
	}
	jitLoader.LoadOne(r.Text, "jjs_marshaller", 0, 40, []bool{false, true, true, true}, nil)
	return r
}

func (p *Program) insertIndexInc(objl *objLink, builder *program, size int, from obj.Addr) *objLink {
	if size >= 0 {
		objl.Prog = &obj.Prog{
			As: x86.AADDQ,
			From: obj.Addr{
				Type:   obj.TYPE_CONST,
				Offset: int64(size),
			},
			To: _RDX,
		}
	} else {
		objl.Prog = &obj.Prog{
			As:   x86.AADDQ,
			From: from,
			To:   _RDX,
		}
	}
	objl, _ = builder.AppendP(objl)
	return objl
}

func (p *Program) outputSingleByteIR(objl *objLink, builder *program, bytesBaseLen *int, b byte) *objLink {
	objl = buildMov(builder, objl, "byte", obj.Addr{
		Type:   obj.TYPE_CONST,
		Offset: int64(b),
	}, obj.Addr{
		Type:   obj.TYPE_MEM,
		Reg:    x86.REG_AX,
		Index:  x86.REG_DX,
		Scale:  1,
		Offset: int64(*bytesBaseLen),
	})
	*bytesBaseLen += 1
	return objl
}

func (p *Program) outputStringIR(objl *objLink, builder *program, bytesBaseLen *int, s1 string) *objLink {
	strLen := len(s1)
	var index int
	for index < len(s1) {
		var write int
		switch {
		// go-assembler似乎不允许写入超过4 Byte的常量, 所以不写入8 Byte数据
		case strLen&3 == 0:
			write = 4
		case strLen&1 == 0:
			write = 2
		case strLen > 0:
			write = 1
		}
		objl = buildMovStaticString(builder, objl, write, []byte(s1[index:index+write]), int64(*bytesBaseLen))
		index += write
		strLen -= write
		*bytesBaseLen += write
	}
	return objl
}

func (p *Program) outputDynamicStringIR(objl *objLink, builder *program, valOffset int, bytesBaseLen *int, typ reflect.Type) *objLink {
	switch typ.Kind() {
	case reflect.String:
		rtslen := obj.Addr{
			Type:   obj.TYPE_MEM,
			Reg:    x86.REG_BX,
			Offset: int64(valOffset + 8), // link StringHeader
		}
		rtsptr := obj.Addr{
			Type:   obj.TYPE_MEM,
			Reg:    x86.REG_BX,
			Offset: int64(valOffset), // link StringHeader
		}
		afterProgs := []obj.Prog{
			{As: x86.ALEAQ, From: obj.Addr{Type: obj.TYPE_MEM, Reg: x86.REG_AX, Index: x86.REG_DX, Scale: 1, Offset: int64(*bytesBaseLen)}, To: _RAX},
			{As: x86.AMOVQ, From: rtslen, To: _RCX},
			{As: x86.AMOVQ, From: rtsptr, To: _RBX},
		}
		return p.insertIndexInc(buildCallOnConst(builder, objl, "jjs_jit_memory_copy", nil, afterProgs), builder, -1, rtslen)
	default:
		panic(fmt.Sprintf("not support type %s", typ.Kind()))
	}
}

func (p *Program) compileUnmarshal() []byte {
	return nil
}

func bytes2Int64(b []byte) (val int64) {
	var shift int
	for len(b) != 0 {
		val |= int64(b[0]) << shift
		shift += 8
		b = b[1:]
	}
	return
}
