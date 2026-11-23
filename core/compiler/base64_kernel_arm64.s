#include "textflag.h"

// The four ABI loads and RET delimit the relocatable kernel copied into JIT
// code by genbase64. Keep the kernel free of calls and PC-relative data loads.
// R0 = destination, R1 = source, R2 = length, R3 = standard alphabet.
// Clobbers R0-R7, V0-V6, V16-V20. Does not touch JIT context/data R8/R9.
TEXT ·base64EncodeNEON(SB), NOSPLIT|NOFRAME, $0-32
	MOVD dst+0(FP), R0
	MOVD src+8(FP), R1
	MOVD length+16(FP), R2
	MOVD alphabet+24(FP), R3

	CMP $48, R2
	BLT scalar
	VLD1 (R3), [V16.B16, V17.B16, V18.B16, V19.B16]
	VMOVI $63, V20.B16
vector:
	VLD3.P 48(R1), [V0.B16, V1.B16, V2.B16]
	VUSHR $2, V0.B16, V3.B16
	VUSHR $4, V1.B16, V4.B16
	VSLI $4, V0.B16, V4.B16
	VAND V20.B16, V4.B16, V4.B16
	VUSHR $6, V2.B16, V5.B16
	VSLI $2, V1.B16, V5.B16
	VAND V20.B16, V5.B16, V5.B16
	VAND V20.B16, V2.B16, V6.B16
	VTBL V3.B16, [V16.B16, V17.B16, V18.B16, V19.B16], V3.B16
	VTBL V4.B16, [V16.B16, V17.B16, V18.B16, V19.B16], V4.B16
	VTBL V5.B16, [V16.B16, V17.B16, V18.B16, V19.B16], V5.B16
	VTBL V6.B16, [V16.B16, V17.B16, V18.B16, V19.B16], V6.B16
	VST4.P [V3.B16, V4.B16, V5.B16, V6.B16], 64(R0)
	SUB $48, R2
	CMP $48, R2
	BGE vector
scalar:
	CMP $3, R2
	BLT tail
triple:
	MOVBU (R1), R4
	MOVBU 1(R1), R5
	MOVBU 2(R1), R6
	LSR $2, R4, R7
	MOVBU (R3)(R7), R7
	MOVB R7, (R0)
	LSL $4, R4
	ORR R5>>4, R4, R4
	AND $63, R4
	MOVBU (R3)(R4), R4
	MOVB R4, 1(R0)
	LSL $2, R5
	ORR R6>>6, R5, R5
	AND $63, R5
	MOVBU (R3)(R5), R5
	MOVB R5, 2(R0)
	AND $63, R6
	MOVBU (R3)(R6), R6
	MOVB R6, 3(R0)
	ADD $3, R1
	ADD $4, R0
	SUB $3, R2
	CMP $3, R2
	BGE triple
tail:
	CBZ R2, end
	MOVBU (R1), R4
	LSR $2, R4, R7
	MOVBU (R3)(R7), R7
	MOVB R7, (R0)
	LSL $4, R4
	AND $63, R4
	MOVD $61, R6
	CMP $1, R2
	BEQ one
	MOVBU 1(R1), R5
	ORR R5>>4, R4, R4
	LSL $2, R5
	AND $63, R5
	MOVBU (R3)(R5), R5
	MOVB R5, 2(R0)
	B finish
one:
	MOVB R6, 2(R0)
finish:
	MOVBU (R3)(R4), R4
	MOVB R4, 1(R0)
	MOVB R6, 3(R0)
	ADD $4, R0
end:
	RET
