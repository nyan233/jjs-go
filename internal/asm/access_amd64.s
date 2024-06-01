
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

TEXT ·ComposeRegGoCtxt(SB),NOSPLIT, $0-128
    MOVQ    AX, reg_ax+0(FP)
    MOVQ    BX, reg_bx+8(FP)
    MOVQ    CX, reg_cx+16(FP)
    MOVQ    DX, reg_dx+24(FP)
    MOVQ    R8, reg_r8+32(FP)
    MOVQ    R9, reg_r9+40(FP)
    MOVQ    R10, reg_10+48(FP)
    MOVQ    R11, reg_r11+56(FP)
    MOVQ    R12, reg_r12+64(FP)
    MOVQ    R13, reg_r13+72(FP)
    MOVQ    R14, reg_r14+80(FP)
    MOVQ    R15, reg_r15+88(FP)
    MOVQ    SP, reg_sp+96(FP)
    MOVQ    BP, reg_bp+104(FP)
    XORQ    AX, AX
    BYTE    $0x48;BYTE $0x8B;BYTE $0x05;WORD $0x00;WORD $0x00 // mov [rip+0], rax
    SUBQ    $8, AX
    MOVQ    AX, reg_ip+112(FP)
    RET
