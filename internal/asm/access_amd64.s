
#include "textflag.h"
#include "go_asm.h"

TEXT ·GetR9(SB),NOSPLIT, $0-8
    MOVQ    R9, ret+0(FP)
    RET

TEXT ·JitGetAX(SB),NOSPLIT, $0-0
    MOVQ    AX, R9
    RET

TEXT ·JitGetBX(SB),NOSPLIT, $0-0
    MOVQ    BX, R9
    RET

TEXT ·JitGetCX(SB),NOSPLIT, $0-0
    MOVQ    CX, R9
    RET

TEXT ·JitGetDX(SB),NOSPLIT, $0-0
    MOVQ    DX, R9
    RET

TEXT ·JitGetSP(SB),NOSPLIT, $0-0
    MOVQ    SP, R9
    RET

TEXT ·JitGetBP(SB),NOSPLIT, $0-0
    MOVQ    BP, R9
    RET

TEXT ·JitGetIP(SB),NOSPLIT, $0-0
    MOVQ    $0, AX
    BYTE    $0x48;BYTE $0x8B;BYTE $0x05;WORD $0x00;WORD $0x00 // mov [rip+0], rax
    SUBQ    $8, AX
    MOVQ    AX, R9
    RET

TEXT ·ComposeRegGoCtxt(SB),NOSPLIT, $0-120
    MOVQ    AX, ret_RAX+0(FP)
    MOVQ    BX, ret_RBX+8(FP)
    MOVQ    CX, ret_RCX+16(FP)
    MOVQ    DX, ret_RDX+24(FP)
    MOVQ    R8, ret_R8+32(FP)
    MOVQ    R9, ret_R9+40(FP)
    MOVQ    R10, ret_R10+48(FP)
    MOVQ    R11, ret_R11+56(FP)
    MOVQ    R12, ret_R12+64(FP)
    MOVQ    R13, ret_R13+72(FP)
    MOVQ    R14, ret_R14+80(FP)
    MOVQ    R15, ret_R15+88(FP)
    MOVQ    SP, ret_RSP+96(FP)
    MOVQ    BP, ret_RBP+104(FP)
    XORQ    AX, AX
    BYTE    $0x48;BYTE $0x8B;BYTE $0x05;WORD $0x00;WORD $0x00 // mov [rip+0], rax
    SUBQ    $8, AX
    MOVQ    AX, ret_RIP+112(FP)
    RET
