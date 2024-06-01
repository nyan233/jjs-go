#include "textflag.h"
#include "go_asm.h"

TEXT ·CallTest(SB),NOSPLIT, $0-8
    MOVQ    fn+0(FP), AX
    CALL    AX
    RET

TEXT ·GetPC(SB), NOSPLIT, $0-8
    // BYTE    $0x48;BYTE $0x8B;BYTE $0x05;WORD $0x00;WORD $0x00 // mov rax, [rip+0]
    BYTE    $0x48;BYTE $0x8d;BYTE $05;WORD $0x00;WORD $0x00 // lea rax, [rip+0]
    // MOVQ    (SP), AX
    MOVQ    AX, ret+0(FP)
    RET
