#include "textflag.h"

TEXT ·avx2FindJSONEscape(SB), NOSPLIT, $0-24
    MOVQ    ptr+0(FP), SI
    MOVQ    n+8(FP), BX
    XORQ    DI, DI
    CMPQ    BX, $32
    JL      tail

    MOVQ    $0x22, R8
    MOVD    R8, X0
    VPBROADCASTB X0, Y1
    MOVQ    $0x5c, R8
    MOVD    R8, X0
    VPBROADCASTB X0, Y2
    MOVQ    $0x3c, R8
    MOVD    R8, X0
    VPBROADCASTB X0, Y3
    MOVQ    $0x3e, R8
    MOVD    R8, X0
    VPBROADCASTB X0, Y4
    MOVQ    $0x26, R8
    MOVD    R8, X0
    VPBROADCASTB X0, Y5
    MOVQ    $0x1f, R8
    MOVD    R8, X0
    VPBROADCASTB X0, Y6
    LEAQ    -32(BX), R13

vector_loop:
    VMOVDQU (SI)(DI*1), Y0

    VPCMPEQB Y1, Y0, Y7
    VPMOVMSKB Y7, R11
    VPCMPEQB Y2, Y0, Y7
    VPMOVMSKB Y7, R10
    ORQ     R10, R11
    VPCMPEQB Y3, Y0, Y7
    VPMOVMSKB Y7, R10
    ORQ     R10, R11
    VPCMPEQB Y4, Y0, Y7
    VPMOVMSKB Y7, R10
    ORQ     R10, R11
    VPCMPEQB Y5, Y0, Y7
    VPMOVMSKB Y7, R10
    ORQ     R10, R11
    VPMINUB Y0, Y6, Y7
    VPCMPEQB Y7, Y0, Y7
    VPMOVMSKB Y7, R10
    ORQ     R10, R11
    VPMOVMSKB Y0, R10
    ORQ     R10, R11

    TESTQ   R11, R11
    JNZ     found_mask
    ADDQ    $32, DI
    CMPQ    DI, R13
    JLE     vector_loop

tail:
    CMPQ    DI, BX
    JGE     not_found
    MOVBLZX (SI)(DI*1), R8
    CMPB    R8, $0x20
    JB      found_tail
    CMPB    R8, $0x80
    JAE     found_tail
    CMPB    R8, $0x22
    JE      found_tail
    CMPB    R8, $0x5c
    JE      found_tail
    CMPB    R8, $0x3c
    JE      found_tail
    CMPB    R8, $0x3e
    JE      found_tail
    CMPB    R8, $0x26
    JE      found_tail
    INCQ    DI
    JMP     tail

found_mask:
    BSFQ    R11, R11
    ADDQ    DI, R11
    MOVQ    R11, ret+16(FP)
    VZEROUPPER
    RET

found_tail:
    MOVQ    DI, ret+16(FP)
    VZEROUPPER
    RET

not_found:
    MOVQ    $-1, ret+16(FP)
    VZEROUPPER
    RET

// avx2FindJSONStringEnd scans for bytes that need scalar JSON string handling
// during decoding. Unlike the encoder scanner, it does not treat <, > or & as
// special because JSON decoding leaves those bytes unchanged.
TEXT ·avx2FindJSONStringEnd(SB), NOSPLIT, $0-24
    MOVQ    ptr+0(FP), SI
    MOVQ    n+8(FP), BX
    XORQ    DI, DI
    CMPQ    BX, $32
    JL      string_tail

    MOVQ    $0x22, R8
    MOVD    R8, X0
    VPBROADCASTB X0, Y1
    MOVQ    $0x5c, R8
    MOVD    R8, X0
    VPBROADCASTB X0, Y2
    MOVQ    $0x1f, R8
    MOVD    R8, X0
    VPBROADCASTB X0, Y3
    LEAQ    -32(BX), R13

string_vector_loop:
    VMOVDQU (SI)(DI*1), Y0

    VPCMPEQB Y1, Y0, Y7
    VPMOVMSKB Y7, R11
    VPCMPEQB Y2, Y0, Y7
    VPMOVMSKB Y7, R10
    ORQ     R10, R11
    VPMINUB Y0, Y3, Y7
    VPCMPEQB Y7, Y0, Y7
    VPMOVMSKB Y7, R10
    ORQ     R10, R11
    VPMOVMSKB Y0, R10
    ORQ     R10, R11

    TESTQ   R11, R11
    JNZ     string_found_mask
    ADDQ    $32, DI
    CMPQ    DI, R13
    JLE     string_vector_loop

string_tail:
    CMPQ    DI, BX
    JGE     string_not_found
    MOVBLZX (SI)(DI*1), R8
    CMPB    R8, $0x20
    JB      string_found_tail
    CMPB    R8, $0x80
    JAE     string_found_tail
    CMPB    R8, $0x22
    JE      string_found_tail
    CMPB    R8, $0x5c
    JE      string_found_tail
    INCQ    DI
    JMP     string_tail

string_found_mask:
    BSFQ    R11, R11
    ADDQ    DI, R11
    MOVQ    R11, ret+16(FP)
    VZEROUPPER
    RET

string_found_tail:
    MOVQ    DI, ret+16(FP)
    VZEROUPPER
    RET

string_not_found:
    MOVQ    $-1, ret+16(FP)
    VZEROUPPER
    RET
