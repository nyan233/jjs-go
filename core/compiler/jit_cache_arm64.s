#include "textflag.h"

// flushJITCode invalidates data and instruction caches after JIT text is
// written and made executable. Apple silicon requires this before executing
// newly written instructions.
TEXT ·flushJITCode(SB), NOSPLIT, $0-16
    MOVD    addr+0(FP), R0
    MOVD    size+8(FP), R1
    MOVD    R0, R3
    ADD     R1, R0, R2

dc_loop:
    WORD    $0xd50b7b20 // DC CVAU, R0
    ADD     $64, R0, R0
    CMP     R2, R0
    BLO     dc_loop
    WORD    $0xd5033b9f // DSB ISH

    MOVD    R3, R0
ic_loop:
    WORD    $0xd50b7520 // IC IVAU, R0
    ADD     $64, R0, R0
    CMP     R2, R0
    BLO     ic_loop
    WORD    $0xd5033b9f // DSB ISH
    WORD    $0xd5033fdf // ISB
    RET
