package compile

import (
	"github.com/twitchyliquid64/golang-asm/obj"
	"github.com/twitchyliquid64/golang-asm/obj/x86"
	"runtime"
)

type RegAlign struct {
	Src  obj.Addr
	Dest obj.Addr
}

func buildBytes2String(ctxt *program, link *objLink, bPtr, bLen, sPtr, sLen obj.Addr) *objLink {
	link.Prog = &obj.Prog{
		As:   x86.AMOVQ,
		From: bPtr,
		To:   sPtr,
	}
	link, _ = ctxt.AppendP(link)
	link.Prog = &obj.Prog{
		As:   x86.AMOVQ,
		From: bLen,
		To:   sLen,
	}
	link, _ = ctxt.AppendP(link)
	return link
}

func buildCallMemoryCopy(ctxt *program, link *objLink) *objLink {
	return buildCallOnConst(ctxt, link, "jjs_jit_memory_copy", nil, nil)
}

// 对齐操作发生在栈保存之后, 不需要栈保存的情况也不会触发对齐
func buildCallOnConst(ctxt *program, link *objLink, name string, beforeProgs, afterProgs []obj.Prog) *objLink {
	desc := getFuncAddrByName(name)
	if desc.Name == "" {
		return nil
	}
	for k := range beforeProgs {
		link.Prog = &beforeProgs[k]
		link, _ = ctxt.AppendP(link)
	}
	if stackSize := len(desc.SaveRegs) * 8; stackSize > 0 {
		link.Prog = &obj.Prog{
			As: x86.ASUBQ,
			From: obj.Addr{
				Type:   obj.TYPE_CONST,
				Offset: int64(stackSize),
			},
			To: _R8,
		}
		link, _ = ctxt.AppendP(link)
		for key, val := range desc.SaveRegs {
			link.Prog = &obj.Prog{
				As:   x86.AMOVQ,
				From: val,
				To: obj.Addr{
					Type:   obj.TYPE_MEM,
					Reg:    x86.REG_R8,
					Offset: int64(key * 8),
				},
			}
			link, _ = ctxt.AppendP(link)
		}
		for k := range afterProgs {
			link.Prog = &afterProgs[k]
			link, _ = ctxt.AppendP(link)
		}
		link = buildACALL(ctxt, link, desc)
		for key, val := range desc.SaveRegs {
			link.Prog = &obj.Prog{
				As: x86.AMOVQ,
				From: obj.Addr{
					Type:   obj.TYPE_MEM,
					Reg:    x86.REG_R8,
					Offset: int64(key * 8),
				},
				To: val,
			}
			link, _ = ctxt.AppendP(link)
		}
		link.Prog = &obj.Prog{
			As: x86.AADDQ,
			From: obj.Addr{
				Type:   obj.TYPE_CONST,
				Offset: int64(stackSize),
			},
			To: _R8,
		}
		link, _ = ctxt.AppendP(link)
	} else {
		link = buildACALL(ctxt, link, desc)
	}
	return link
}

func buildStringMemoryCopy(ctxt *program, link *objLink) *objLink {
	return nil
}

func buildMovStaticString(ctxt *program, link *objLink, width int, b1 []byte, offset int64) *objLink {
	var level string
	offsetFrom := obj.Addr{
		Type:   obj.TYPE_CONST,
		Offset: bytes2Int64(b1),
	}
	switch width {
	case 1:
		level = "byte"
	case 2:
		level = "word"
	case 4:
		level = "long"
	case 8:
		level = "quad"
	}
	return buildMov(ctxt, link, level, offsetFrom, obj.Addr{
		Type:   obj.TYPE_MEM,
		Reg:    x86.REG_AX,
		Index:  x86.REG_DX,
		Scale:  1,
		Offset: offset,
	})
}

func buildMov(ctxt *program, link *objLink, level string, from, to obj.Addr) *objLink {
	switch level {
	case "byte":
		link.Prog.As = x86.AMOVB
	case "word":
		link.Prog.As = x86.AMOVW
	case "long":
		link.Prog.As = x86.AMOVL
	case "quad":
		link.Prog.As = x86.AMOVQ
	default:
		panic("unknown instruction")
	}
	link.Prog.From = from
	link.Prog.To = to
	link, _ = ctxt.AppendP(link)
	return link
}

func buildACALL(ctxt *program, objl *objLink, desc funcDesc) *objLink {
	var (
		AbleUsage = []int{x86.REG_CX, x86.REG_DX, x86.REG_DI, x86.REG_SI}
	)
	if ctxt == nil && objl == nil {
		ctxt = newProgram()
		objl, _ = ctxt.AppendP(nil)
	}
	to := _RCX
	for _, val := range AbleUsage {
		for _, val2 := range desc.MinUseRegs {
			if val2.Type != obj.TYPE_REG {
				continue
			}
			if val != int(val2.Reg) {
				to = obj.Addr{
					Type: obj.TYPE_REG,
					Reg:  int16(val),
				}
				break
			}
		}
	}
	objl.Prog = &obj.Prog{
		As: x86.AMOVQ,
		From: obj.Addr{
			Type:   obj.TYPE_CONST,
			Offset: int64(desc.UnsafeAddress),
		},
		To: to,
	}
	objl, _ = ctxt.AppendP(objl)
	objl.Prog = &obj.Prog{
		As: obj.ACALL,
		To: to,
	}
	objl, _ = ctxt.AppendP(objl)
	return objl
}

func loadG(ctxt *program, link *objLink) {
	switch runtime.GOOS {
	case "windows", "plan9":
		break
	case "linux":
		break
	}
}
