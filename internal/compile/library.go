package compile

import (
	"github.com/twitchyliquid64/golang-asm/obj"
	"github.com/twitchyliquid64/golang-asm/obj/x86"
	"math"
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
	to := obj.Addr{
		Type:   obj.TYPE_MEM,
		Reg:    x86.REG_AX,
		Index:  x86.REG_DX,
		Scale:  1,
		Offset: offset,
	}
	switch width {
	case 1:
		level = "byte"
	case 2:
		level = "word"
	case 4:
		level = "long"
	case 8:
		return buildQuadMovOnConst(ctxt, link, offsetFrom.Offset, to)
	}
	return buildMov(ctxt, link, level, offsetFrom, to)
}

func buildMov(ctxt *program, link *objLink, level string, from, to obj.Addr) *objLink {
	switch level {
	case "byte":
		link.Prog.As = x86.AMOVB
	case "word":
		link.Prog.As = x86.AMOVW
	case "long":
		link.Prog.As = x86.AMOVL
		panic("unknown instruction")
	}
	link.Prog.From = from
	link.Prog.To = to
	link, _ = ctxt.AppendP(link)
	return link
}

// movq不支持64位立即数, 所以需要movabs中转
// 以下生成的指令等于: movabs rcx, $val
//
//	mov	rcx, [to_addr]
func buildQuadMovOnConst(ctxt *program, link *objLink, val int64, to obj.Addr) *objLink {
	link.Prog.As = x86.AWORD
	link.Prog.From = obj.Addr{
		Type:   obj.TYPE_CONST,
		Offset: 0xb948,
	}
	link, _ = ctxt.AppendP(link)
	link.Prog.As = x86.AQUAD
	link.Prog.From = obj.Addr{
		Type:   obj.TYPE_CONST,
		Offset: val,
	}
	link, _ = ctxt.AppendP(link)
	link.Prog.As = x86.AMOVQ
	link.Prog.From = _RCX
	link.Prog.To = to
	link, _ = ctxt.AppendP(link)
	return link
}

func buildACALL(ctxt *program, objl *objLink, desc funcDesc) *objLink {
	var (
		AbleUsage = []int{x86.REG_DX, x86.REG_R12, x86.REG_R13}
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

func buildSimd32Memmove(ctxt *program, objl *objLink, startPtr uintptr, baseLen, n int) *objLink {
	if startPtr+uintptr(n*32) > uintptr(math.MaxUint32) {
		objl = buildQuadMovOnConst(ctxt, objl, int64(startPtr), _RCX)
	} else {
		objl = buildMov(ctxt, objl, "long", obj.Addr{
			Type:   obj.TYPE_CONST,
			Offset: int64(startPtr),
		}, _RCX)
	}
	for i := 0; i < 16; i++ {
		if i == n {
			break
		}
		objl.Prog = &obj.Prog{
			As: x86.AVMOVDQU,
			From: obj.Addr{
				Type:   obj.TYPE_MEM,
				Reg:    x86.REG_CX,
				Offset: int64(i * 32),
			},
			To: obj.Addr{
				Type: obj.TYPE_REG,
				Reg:  int16(x86.REG_Y0 + i),
			},
		}
		objl, _ = ctxt.AppendP(objl)
	}
	for i := 0; i < 16; i++ {
		if i == n {
			break
		}
		objl.Prog = &obj.Prog{
			As: x86.AVMOVDQU,
			From: obj.Addr{
				Type: obj.TYPE_REG,
				Reg:  int16(x86.REG_Y0 + i),
			},
			To: obj.Addr{
				Type:   obj.TYPE_MEM,
				Reg:    x86.REG_AX,
				Index:  x86.REG_DX,
				Scale:  1,
				Offset: int64(baseLen + i*32),
			},
		}
		objl, _ = ctxt.AppendP(objl)
	}
	if n > 16 {
		return buildSimd32Memmove(ctxt, objl, startPtr+uintptr(16*32), baseLen+16*32, n-16)
	}
	return objl
}

func buildGrowSlice(ctxt *program, objl *objLink, from obj.Addr, incr int64) *objLink {
	objl.Prog = &obj.Prog{
		As:   x86.AMOVQ,
		From: from,
		To:   _RCX,
	}
	objl, _ = ctxt.AppendP(objl)
	objl.Prog = &obj.Prog{
		As: x86.AADDQ,
		From: obj.Addr{
			Type:   obj.TYPE_CONST,
			Offset: incr,
		},
		To: _RCX,
	}
	objl, _ = ctxt.AppendP(objl)
	objl.Prog = &obj.Prog{
		As:   x86.AADDQ,
		From: _RDX,
		To:   _RCX,
	}
	objl, _ = ctxt.AppendP(objl)
	objl.Prog = &obj.Prog{
		As:   x86.ACMPQ,
		From: _RCX,
		To:   _RSI,
	}
	objl.Prog = &obj.Prog{}
	// jge ?
	objl, _ = ctxt.AppendP(objl)
	objl.Prog = &obj.Prog{
		As: x86.AWORD,
		From: obj.Addr{
			Type:   obj.TYPE_CONST,
			Offset: 0x8d0f, // 0f 8d on little-endian
		},
	}
	objl, _ = ctxt.AppendP(objl)
	objl.Prog = &obj.Prog{
		As: x86.ALONG,
		From: obj.Addr{
			Type:   obj.TYPE_CONST,
			Offset: int64(0x12345678),
		},
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
