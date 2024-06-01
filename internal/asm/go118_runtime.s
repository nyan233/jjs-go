#include "textflag.h"


TEXT ·jitGetg(SB),NOSPLIT, $0-0
    MOVQ    R14, R8 // R14 go runtime save g-struct-ptr on amd64
    RET

TEXT ·getg(SB),NOSPLIT, $0-8
    MOVQ    R14, r0+0(FP) // R14 go runtime save g-struct-ptr on amd64
    RET
