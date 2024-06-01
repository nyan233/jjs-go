#include "textflag.h"

// AX: dest-ptr
// BX: source-ptr
// CX: copy-max
// DX: center-state
TEXT ·JitMemoryCopy(SB),NOSPLIT, $0-0
    XORQ    DX, DX
    TESTQ   CX, CX
    JZ      return
    TESTQ   $31, CX // and rcx, $31
    JZ      u_full_copy
copy1:
    MOVB    (BX), R13
    INCQ    BX
    MOVB    R13, (AX)
    INCQ    AX
    DECQ    CX
    TESTQ   $31, CX
    JNZ     copy1
    TESTQ   CX, CX
    JZ      return
u_full_copy:
    // LONG    $0xc5fe6f04;BYTE $0x13;NOP // vmovdqu ymm0, [rbx+rdx*1]
    // LONG    $0xc5fe7f04;BYTE $0x10;NOP // vmovdqu [rax+rdx*1], ymm0
    VMOVDQU (BX)(DX*1), Y0
    VMOVDQU Y0, (AX)(DX*1)
    ADDQ    $32, DX
    CMPQ    CX, DX
    JNE     u_full_copy
return:
    RET

TEXT ·JitSave(SB),NOSPLIT, $0-0
    SUBQ    $48, R8
    MOVQ    AX, 0(R8)
    MOVQ    BX, 8(R8)
    MOVQ    CX, 16(R8)
    MOVQ    DX, 24(R8)
    MOVQ    DI, 32(R8)
    MOVQ    SI, 40(R8)
    RET

TEXT ·JitRecover(SB), NOSPLIT, $0-0
    MOVQ    0(R8), AX
    MOVQ    8(R8), BX
    MOVQ    16(R8), CX
    MOVQ    24(R8), DX
    MOVQ    32(R8), DI
    MOVQ    40(R8), SI
    ADDQ    $48, R8
    RET

TEXT ·CallMFunc(SB), NOSPLIT, $0-56
    MOVQ    text+0(FP), DX
    MOVQ    stack+8(FP), R8
    MOVQ    dest_ptr+16(FP), AX
    MOVQ    data_ptr+24(FP), BX
    MOVQ    0(DX), SI
    XORQ    DX, DX
    CALL    SI
    MOVQ    DX, write+32(FP)
    MOVQ    $0x20, reason+40(FP)
    RET

// ABIInternal
TEXT ·WapperGrowSlice(SB), NOSPLIT, $0-0
    SUBQ    $24, R8
    SUBQ    $8, SP
    MOVQ    R8, SP
    MOVQ    R10, 0(R8)

    ADDQ    $8, SP
    JMP     R10
