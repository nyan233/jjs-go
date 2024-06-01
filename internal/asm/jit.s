#include "textflag.h"
#include "funcdata.h"
#include "go_asm.h"

TEXT ·jitCall(SB),NOSPLIT,$72-0
    NO_LOCAL_POINTERS
    MOVQ    8(AX), R8 // jit_code_stack_address
    MOVQ   16(AX), R9 // jit_code_code_address
    CALL     R9
    RET

TEXT ·goCodeJitCall(SB), NOSPLIT, $80-8
    NO_LOCAL_POINTERS
    MOVQ    prog+0(FP), AX
    CALL    ·jitCall(SB)
    RET

TEXT ·entergocall(SB),NOSPLIT,$8-0
    NO_LOCAL_POINTERS
    CALL    runtime·procPin(SB)
    RET

TEXT ·exitgocall(SB),NOSPLIT,$8-0
    NO_LOCAL_POINTERS
    CALL runtime·procUnpin(SB)
    RET

TEXT ·jitstack(SB),NOSPLIT,$24-25
    NO_LOCAL_POINTERS
    MOVQ    ss_lo+0(FP), AX
    MOVQ    ss_hi+8(FP), BX
    MOVQ    func+16(FP), R9
    MOVQ    lock_flag+24(FP), CX
    TESTQ   CX, CX
    JZ      no_lock
    CALL    ·procPin(SB)
    CALL    ·jitstack_switch(SB)
    CALL    ·procUnpin(SB)
no_lock:
    JMP     ·jitstack_switch(SB)
    RET

TEXT ·jitstack_switch(SB),NOSPLIT,$0-0
    MOVQ    (TLS), CX
    MOVQ    g_m(CX), SI
    MOVQ    m_curg(SI), SI
    TESTQ   SI, SI
    CMOVQNE SI, CX // cmovnz rcx, rsi
    MOVQ    BX, R8
    SUBQ    $40, R8 // save-space(40B)
    MOVQ    CX, 32(R8) // (r8) = g
    MOVQ    SP, 24(R8)
    MOVQ    0(CX), DX // rdx = g_stack_lo
    MOVQ    DX, 16(R8)
    MOVQ    8(CX), DX // rdx = g_stack_hi
    MOVQ    DX, 8(R8)
    MOVQ    16(CX), DI // rdi = g_stackguard0
    MOVQ    DI, 0(R8)
    MOVQ    R8, SP
    MOVQ    AX, 0(CX) // g_stack_lo = rax
    MOVQ    BX, 8(CX) // g_stack_hi = rbx
    MOVQ    AX, 16(CX) // g_stackguard0 = rdi
    CALL    (R9)
    MOVQ    SP, R8
    MOVQ    40(R8), CX // rcx = g
    MOVQ    32(R8), SP
    MOVQ    16(R8), DX
    MOVQ    DX, 0(CX)
    MOVQ    8(R8), DX
    MOVQ    DX, 8(CX)
    MOVQ    0(R8), DX
    MOVQ    DX, 16(CX)
    ADDQ    $48, R8
    RET

TEXT ·stacksave<>(SB),NOSPLIT,$0-0
    MOVQ	$jitstack_switch(SB), R9
	MOVQ	R9, (g_sched+gobuf_pc)(R14)
	LEAQ	8(SP), R9
	MOVQ	R9, (g_sched+gobuf_sp)(R14)
	MOVQ	$0, (g_sched+gobuf_ret)(R14)
	MOVQ	BP, (g_sched+gobuf_bp)(R14)
	// Assert ctxt is zero. See func save.
	MOVQ	(g_sched+gobuf_ctxt)(R14), R9
	TESTQ	R9, R9
	JZ	2(PC)
	CALL	runtime·abort(SB)
	RET
