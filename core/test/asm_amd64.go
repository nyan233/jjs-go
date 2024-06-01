package test

import (
	"encoding/hex"
	"github.com/nyan233/jjs-go/internal/sys"
	"github.com/twitchyliquid64/golang-asm"
	"github.com/twitchyliquid64/golang-asm/obj"
	"unsafe"
)

func CallTest(fn uintptr)

func GetPC() uintptr

func GetJitGenCallTest() uintptr {
	builder, err := golangasm.NewBuilder("amd64", 64)
	if err != nil {
		panic(err)
	}
	//builder.AddInstruction(&obj.Prog{
	//	As: x86.AMOVQ,
	//	From: obj.Addr{
	//		Type:   obj.TYPE_CONST,
	//		Offset: int64(5893376),
	//	},
	//	To: obj.Addr{
	//		Type: obj.TYPE_REG,
	//		Reg:  x86.REG_AX,
	//	},
	//})
	builder.AddInstruction(&obj.Prog{
		As: obj.ACALL,
		To: obj.Addr{
			Type:   obj.TYPE_MEM,
			Offset: int64(6252624),
		},
	})
	builder.AddInstruction(&obj.Prog{
		As: obj.ARET,
	})
	buf := builder.Assemble()
	println(hex.EncodeToString(buf))
	buf = sys.ModifyPermissions2Exec(buf)
	return (uintptr)(unsafe.Pointer(&buf[0]))
}
