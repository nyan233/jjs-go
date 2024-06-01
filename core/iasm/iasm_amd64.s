#include "textflag.h"
#include "go_asm.h"

TEXT ·jitgrowslice(SB), NOSPLIT, $0-0
    MOVQ AX, DI // ax = newlen
    MOVQ AX, BX
    SUBQ 8(R9), DI // slice len -> DI
    MOVQ 16(R9), CX // slice cap -> CX
    MOVQ 0(R9), AX // slice ptr -> AX
    LEAQ ·BytesEtype(SB), SI
    CALL runtime·growslice(SB)
    MOVQ AX, 0(R9) // AX -> slice ptr
    MOVQ BX, 8(R9) // BX -> slice len
    MOVQ CX, 16(R9) // CX -> slice cap
    JMP (SP)
