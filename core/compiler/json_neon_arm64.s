#include "textflag.h"

// JSONMASK leaves bit 7 set in each lane containing a quote, backslash,
// control byte or non-ASCII byte. All loads stay within the supplied length.
#define JSONMASK \
	VUSHR $5, V0.B16, V3.B16 \
	VCMEQ V10.B16, V3.B16, V3.B16 \
	VCMEQ V8.B16, V0.B16, V1.B16 \
	VCMEQ V9.B16, V0.B16, V2.B16 \
	VORR V2.B16, V1.B16, V1.B16 \
	VORR V3.B16, V1.B16, V1.B16 \
	VORR V0.B16, V1.B16, V1.B16

TEXT ·scanJSONSpecialNEON(SB), NOSPLIT, $0-32
	MOVD ptr+0(FP), R0
	MOVD n+8(FP), R1
	MOVD html+16(FP), R2
	MOVD R0, R3
	CMP $16, R1
	BLT tail
	MOVD $34, R4
	VMOV R4, V8.B16
	MOVD $92, R4
	VMOV R4, V9.B16
	VEOR V10.B16, V10.B16, V10.B16
	MOVD $128, R4
	VMOV R4, V11.B16
	CBZ R2, decode_loop
	MOVD $60, R4
	VMOV R4, V12.B16
	MOVD $62, R4
	VMOV R4, V13.B16
	MOVD $38, R4
	VMOV R4, V14.B16
encode_loop:
	VLD1 (R3), [V0.B16]
	JSONMASK
	VCMEQ V12.B16, V0.B16, V2.B16
	VORR V2.B16, V1.B16, V1.B16
	VCMEQ V13.B16, V0.B16, V2.B16
	VORR V2.B16, V1.B16, V1.B16
	VCMEQ V14.B16, V0.B16, V2.B16
	VORR V2.B16, V1.B16, V1.B16
	VAND V11.B16, V1.B16, V1.B16
	VUADDLV V1.B16, V4
	VMOV V4.H[0], R4
	CBNZ R4, vector_found
	ADD $16, R3
	SUB $16, R1
	CMP $16, R1
	BGE encode_loop
	CBZ R1, none
	// Scan the final 16 bytes with overlap instead of a byte-wise tail.
	// The overlapped prefix is already known to contain no special bytes.
	SUB $16, R1, R4
	ADD R4, R3, R3
	MOVD $16, R1
	B encode_loop
decode_loop:
	VLD1 (R3), [V0.B16]
	JSONMASK
	VAND V11.B16, V1.B16, V1.B16
	VUADDLV V1.B16, V4
	VMOV V4.H[0], R4
	CBNZ R4, vector_found
	ADD $16, R3
	SUB $16, R1
	CMP $16, R1
	BGE decode_loop
	CBZ R1, none
	SUB $16, R1, R4
	ADD R4, R3, R3
	MOVD $16, R1
	B decode_loop
tail:
	CBZ R1, none
	MOVBU (R3), R4
	CMP $32, R4
	BLT scalar_found
	CMP $128, R4
	BGE scalar_found
	CMP $34, R4
	BEQ scalar_found
	CMP $92, R4
	BEQ scalar_found
	CBZ R2, tail_next
	CMP $60, R4
	BEQ scalar_found
	CMP $62, R4
	BEQ scalar_found
	CMP $38, R4
	BEQ scalar_found
tail_next:
	ADD $1, R3
	SUB $1, R1
	B tail
vector_found:
	VMOV V1.D[0], R4
	CBNZ R4, lane
	VMOV V1.D[1], R4
	ADD $8, R3
lane:
	RBIT R4, R4
	CLZ R4, R4
	ADD R4>>3, R3, R3
scalar_found:
	SUB R0, R3, R4
	MOVD R4, ret+24(FP)
	RET
none:
	MOVD $-1, R4
	MOVD R4, ret+24(FP)
	RET
