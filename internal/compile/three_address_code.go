package compile

const (
	IsUsageRegister = iota + 4
)

// TWoAddressCode 简单两地址代码的实现
type TWoAddressCode struct {
	// 真实的机器指令, 对应go-assembler的定义
	Instruction uint16
	Arg0        LIRAddr
	Arg1        LIRAddr
}

type LIRAddr struct {
	Reg    uint16
	Index  uint16
	Scale  uint16
	Offset uint32
	// movabs用于64位常量的移动, 常规mov只支持32位常量
	MovAbsVal uint64
	Label     string
}

type LIRList struct {
	labels                map[string]uint32
	registerAllocInit     uint16
	registerAllocCount    uint16
	ableUsageRegisterList []uint16
	// 基于寄存器的调用规约, 典型x86-64 ABI
	registerABI []uint16
	tCodeList   []TWoAddressCode
}

func (l *LIRList) RequireVirtualRegister() uint16 {
	l.registerAllocCount++
	return l.registerAllocCount
}

func (l *LIRList) reAllocRegister() {
	/*
		基于线性扫描的寄存器分配算法
	*/
	type RegisterLive struct {
		Reg     uint16
		Op      uint16
		IsStart bool
	}
	//regLiveList := make([]RegisterLive, 0, 8)
	for _, tw := range l.tCodeList {
		if tw.Arg1.Index > l.registerAllocInit {

		}
	}
}
