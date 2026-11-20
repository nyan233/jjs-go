package compile

import "github.com/twitchyliquid64/golang-asm/obj/x86"

/*
	简易的寄存器分配算法
*/

var (
	archRegisterList = map[string][]int{
		"amd64": {x86.REG_AX, x86.REG_BX, x86.REG_CX, x86.REG_DX,
			x86.REG_R9, x86.REG_R10, x86.REG_R11, x86.REG_R12, x86.REG_DI, x86.REG_SI},
	}
)

type allocator struct {
	reqCount int
	regMap   map[int]int
	goArch   string
}

func (r *allocator) Request(action int) int {
	return -1
}

func (r *allocator) FuncCall(name string) {

}

func (r *allocator) FunCall2(name string) {

}
