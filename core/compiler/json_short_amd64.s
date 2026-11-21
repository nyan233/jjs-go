#include "textflag.h"

// Byte ranges that require JSON escaping:
// control bytes, '"', '&', '<', '>', '\\', and non-ASCII bytes.
DATA ·jsonEscapeRanges+0(SB)/8, $0x3c3c262622221f00
DATA ·jsonEscapeRanges+8(SB)/8, $0x0000ff805c5c3e3e
GLOBL ·jsonEscapeRanges(SB), RODATA|NOPTR, $16

// sseFindJSONEscape scans strings below the AVX2 threshold. PCMPESTRI handles
// 16 bytes per iteration; VMOVQ and VMOVD handle the 8- and 4-byte tails.
TEXT ·sseFindJSONEscape(SB), NOSPLIT, $0-24
    MOVQ    ptr+0(FP), SI
    MOVQ    n+8(FP), BX
    XORQ    DI, DI
    MOVOU   ·jsonEscapeRanges(SB), X1
    MOVL    $14, AX
    CMPQ    BX, $16
    JL      sse_escape_tail
    MOVL    $16, DX
    LEAQ    -16(BX), R13

sse_escape_loop:
    VMOVDQU (SI)(DI*1), X0
    PCMPESTRI $4, X0, X1
    CMPQ    CX, $16
    JB      sse_escape_found
    ADDQ    $16, DI
    CMPQ    DI, R13
    JLE     sse_escape_loop

sse_escape_tail:
    CMPQ    DI, BX
    JGE     sse_escape_not_found
    MOVQ    BX, R9
    SUBQ    DI, R9
    CMPQ    R9, $8
    JL      sse_escape_dword
    VMOVQ   (SI)(DI*1), X0
    MOVL    $8, DX
    PCMPESTRI $4, X0, X1
    CMPQ    CX, $8
    JB      sse_escape_found
    ADDQ    $8, DI

sse_escape_dword:
    CMPQ    DI, BX
    JGE     sse_escape_not_found
    MOVQ    BX, R9
    SUBQ    DI, R9
    CMPQ    R9, $4
    JL      sse_escape_byte
    VMOVD   (SI)(DI*1), X0
    MOVL    $4, DX
    PCMPESTRI $4, X0, X1
    CMPQ    CX, $4
    JB      sse_escape_found
    ADDQ    $4, DI

sse_escape_byte:
    CMPQ    DI, BX
    JGE     sse_escape_not_found
    MOVBLZX (SI)(DI*1), R8
    CMPB    R8, $0x20
    JB      sse_escape_found_tail
    CMPB    R8, $0x80
    JAE     sse_escape_found_tail
    CMPB    R8, $0x22
    JE      sse_escape_found_tail
    CMPB    R8, $0x5c
    JE      sse_escape_found_tail
    CMPB    R8, $0x3c
    JE      sse_escape_found_tail
    CMPB    R8, $0x3e
    JE      sse_escape_found_tail
    CMPB    R8, $0x26
    JE      sse_escape_found_tail
    INCQ    DI
    JMP     sse_escape_byte

sse_escape_found:
    ADDQ    DI, CX
    MOVQ    CX, ret+16(FP)
    VZEROUPPER
    RET

sse_escape_found_tail:
    MOVQ    DI, ret+16(FP)
    VZEROUPPER
    RET

sse_escape_not_found:
    MOVQ    $-1, ret+16(FP)
    VZEROUPPER
    RET
