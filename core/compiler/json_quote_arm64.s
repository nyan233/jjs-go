#include "textflag.h"
#include "go_asm.h"

// Bounded fused scan/copy/escape. No calls, stack changes or allocation.
// R0/R1: output cursor/end; R2/R3: input cursor/end; R4/R5: original cursors.
// R12: packed ASCII escapes; R13: scalar window end. R18 and Go's g are unused.
TEXT ·quoteJSONString(SB), NOSPLIT, $0-48
	MOVD dst+0(FP), R0
	MOVD capacity+8(FP), R1
	MOVD src+16(FP), R2
	MOVD length+24(FP), R3
	MOVD R2, R4
	MOVD R0, R5
	ADD R0, R1, R1
	// Bound a NOSPLIT call to 64 KiB (plus at most one complete UTF-8 rune).
	// R3 remains the real source end so a rune crossing the budget is valid.
	MOVD $const_quoteChunkBytes, R16
	CMP R16, R3
	CSEL LT, R3, R16, R16
	ADD R2, R16, R16
	ADD R2, R3, R3
	MOVD $·jsonQuoteEscapes(SB), R12
	SUB R2, R3, R6
	CMP $8, R6
	BLT quote_tail
	MOVD $·jsonQuoteClasses(SB), R6
	VLD1 (R6), [V8.B16, V9.B16]
	MOVD $15, R6
	VMOV R6, V10.B16
quote_vector:
	SUB R2, R16, R6
	CMP $16, R6
	BLT quote_vector8
	SUB R0, R1, R6
	CMP $16, R6
	BLT quote_vector8
	VLD1 (R2), [V0.B16]
	VAND V10.B16, V0.B16, V1.B16
	VUSHR $4, V0.B16, V2.B16
	VTBL V1.B16, [V8.B16], V1.B16
	VTBL V2.B16, [V9.B16], V2.B16
	VAND V2.B16, V1.B16, V1.B16
	// Class bits are <=16, so widening addition cannot wrap to zero.
	VUADDLV V1.B16, V3
	VMOV V3.H[0], R6
	CBNZ R6, quote_special_block
	VST1 [V0.B16], (R0)
	ADD $16, R0
	ADD $16, R2
	B quote_vector
quote_vector8:
	SUB R2, R16, R6
	CMP $8, R6
	BLT quote_tail
	SUB R0, R1, R6
	CMP $8, R6
	BLT quote_tail
	VLD1 (R2), [V0.B8]
	VAND V10.B8, V0.B8, V1.B8
	VUSHR $4, V0.B8, V2.B8
	VTBL V1.B8, [V8.B16], V1.B8
	VTBL V2.B8, [V9.B16], V2.B8
	VAND V2.B8, V1.B8, V1.B8
	VUADDLV V1.B8, V3
	VMOV V3.H[0], R6
	CBNZ R6, quote_special_eight
	VST1 [V0.B8], (R0)
	ADD $8, R0
	ADD $8, R2
	B quote_vector
quote_special_eight:
	ADD $8, R2, R13
	B quote_scalar
quote_special_block:
	// Avoid repeating vector classification on densely escaped input.
	ADD $16, R2, R13
	B quote_scalar
quote_tail:
	MOVD R16, R13
quote_scalar:
	CMP R16, R2
	BHS quote_done
	CMP R1, R0
	BHS quote_done
	MOVBU (R2), R6
	CMP $128, R6
	BHS quote_utf8
	MOVD (R12)(R6<<3), R11
	CBNZ R11, quote_ascii_escape
	MOVB R6, (R0)
	ADD $1, R2
	ADD $1, R0
quote_next:
	CMP R13, R2
	BLO quote_scalar
	B quote_vector
quote_ascii_escape:
	LSR $56, R11, R7
	MOVD $1, R15
	B quote_escape

// Validate UTF-8 without decoding a code point. Invalid input consumes one
// byte, matching encoding/json's replacement behavior. Read only after checking
// the complete sequence fits in the input, including on truncated page tails.
quote_utf8:
	CMP $0xc2, R6
	BLO quote_invalid
	CMP $0xe0, R6
	BLO quote_utf8_two
	CMP $0xf0, R6
	BLO quote_utf8_three
	CMP $0xf5, R6
	BHS quote_invalid
	SUB R2, R3, R7
	CMP $4, R7
	BLO quote_invalid
	MOVBU 1(R2), R8
	SUB $0x80, R8, R9
	CMP $0x40, R9
	BHS quote_invalid
	CMP $0xf0, R6
	BNE quote_utf8_four_max
	CMP $0x90, R8
	BLO quote_invalid
quote_utf8_four_max:
	CMP $0xf4, R6
	BNE quote_utf8_four_tail
	CMP $0x90, R8
	BHS quote_invalid
quote_utf8_four_tail:
	MOVBU 2(R2), R8
	SUB $0x80, R8, R9
	CMP $0x40, R9
	BHS quote_invalid
	MOVBU 3(R2), R8
	SUB $0x80, R8, R9
	CMP $0x40, R9
	BHS quote_invalid
	SUB R0, R1, R9
	CMP $4, R9
	BLO quote_done
	MOVWU (R2), R11
	MOVW R11, (R0)
	ADD $4, R2
	ADD $4, R0
	B quote_next
quote_utf8_two:
	SUB R2, R3, R7
	CMP $2, R7
	BLO quote_invalid
	MOVBU 1(R2), R8
	SUB $0x80, R8, R9
	CMP $0x40, R9
	BHS quote_invalid
	SUB R0, R1, R9
	CMP $2, R9
	BLO quote_done
	MOVHU (R2), R11
	MOVH R11, (R0)
	ADD $2, R2
	ADD $2, R0
	B quote_next
quote_utf8_three:
	SUB R2, R3, R7
	CMP $3, R7
	BLO quote_invalid
	MOVBU 1(R2), R8
	SUB $0x80, R8, R9
	CMP $0x40, R9
	BHS quote_invalid
	CMP $0xe0, R6
	BNE quote_utf8_three_max
	CMP $0xa0, R8
	BLO quote_invalid
quote_utf8_three_max:
	CMP $0xed, R6
	BNE quote_utf8_three_tail
	CMP $0xa0, R8
	BHS quote_invalid
quote_utf8_three_tail:
	MOVBU 2(R2), R10
	SUB $0x80, R10, R9
	CMP $0x40, R9
	BHS quote_invalid
	CMP $0xe2, R6
	BNE quote_copy_three
	CMP $0x80, R8
	BNE quote_copy_three
	AND $0xfe, R10, R9
	CMP $0xa8, R9
	BEQ quote_separator
quote_copy_three:
	SUB R0, R1, R9
	CMP $3, R9
	BLO quote_done
	MOVHU (R2), R11
	MOVH R11, (R0)
	MOVB R10, 2(R0)
	ADD $3, R2
	ADD $3, R0
	B quote_next
quote_separator:
	MOVD $0x38323032755c, R11 // "\u2028"
	AND $1, R10, R9
	ORR R9<<40, R11, R11
	MOVD $3, R15
	MOVD $6, R7
	B quote_escape
quote_invalid:
	MOVD $0x64666666755c, R11 // "\ufffd"
	MOVD $1, R15
	MOVD $6, R7
quote_escape:
	SUB R0, R1, R9
	CMP R7, R9
	BLO quote_done
	CMP $2, R7
	BEQ quote_escape_two
	MOVW R11, (R0)
	LSR $32, R11, R8
	MOVH R8, 4(R0)
	ADD $6, R0
	ADD R15, R2, R2
	B quote_next
quote_escape_two:
	MOVH R11, (R0)
	ADD $2, R0
	ADD $1, R2
	B quote_next
quote_done:
	SUB R4, R2, R6
	SUB R5, R0, R7
	MOVD R6, consumed+32(FP)
	MOVD R7, written+40(FP)
	RET

// Low/high-nibble class intersection flags controls, quote, backslash, HTML
// delimiters and non-ASCII. A zero intersection is safe to copy unchanged.
DATA ·jsonQuoteClasses+0(SB)/8, $0x1113111111131111
DATA ·jsonQuoteClasses+8(SB)/8, $0x1115111d11111111
DATA ·jsonQuoteClasses+16(SB)/8, $0x0000080004020101
DATA ·jsonQuoteClasses+24(SB)/8, $0x1010101010101010
GLOBL ·jsonQuoteClasses(SB), RODATA|NOPTR, $32

// Packed escape bytes, with output length in the top byte; zero means safe.
DATA ·jsonQuoteEscapes+0(SB)/8, $0x060030303030755c
DATA ·jsonQuoteEscapes+8(SB)/8, $0x060031303030755c
DATA ·jsonQuoteEscapes+16(SB)/8, $0x060032303030755c
DATA ·jsonQuoteEscapes+24(SB)/8, $0x060033303030755c
DATA ·jsonQuoteEscapes+32(SB)/8, $0x060034303030755c
DATA ·jsonQuoteEscapes+40(SB)/8, $0x060035303030755c
DATA ·jsonQuoteEscapes+48(SB)/8, $0x060036303030755c
DATA ·jsonQuoteEscapes+56(SB)/8, $0x060037303030755c
DATA ·jsonQuoteEscapes+64(SB)/8, $0x020000000000625c
DATA ·jsonQuoteEscapes+72(SB)/8, $0x020000000000745c
DATA ·jsonQuoteEscapes+80(SB)/8, $0x0200000000006e5c
DATA ·jsonQuoteEscapes+88(SB)/8, $0x060062303030755c
DATA ·jsonQuoteEscapes+96(SB)/8, $0x020000000000665c
DATA ·jsonQuoteEscapes+104(SB)/8, $0x020000000000725c
DATA ·jsonQuoteEscapes+112(SB)/8, $0x060065303030755c
DATA ·jsonQuoteEscapes+120(SB)/8, $0x060066303030755c
DATA ·jsonQuoteEscapes+128(SB)/8, $0x060030313030755c
DATA ·jsonQuoteEscapes+136(SB)/8, $0x060031313030755c
DATA ·jsonQuoteEscapes+144(SB)/8, $0x060032313030755c
DATA ·jsonQuoteEscapes+152(SB)/8, $0x060033313030755c
DATA ·jsonQuoteEscapes+160(SB)/8, $0x060034313030755c
DATA ·jsonQuoteEscapes+168(SB)/8, $0x060035313030755c
DATA ·jsonQuoteEscapes+176(SB)/8, $0x060036313030755c
DATA ·jsonQuoteEscapes+184(SB)/8, $0x060037313030755c
DATA ·jsonQuoteEscapes+192(SB)/8, $0x060038313030755c
DATA ·jsonQuoteEscapes+200(SB)/8, $0x060039313030755c
DATA ·jsonQuoteEscapes+208(SB)/8, $0x060061313030755c
DATA ·jsonQuoteEscapes+216(SB)/8, $0x060062313030755c
DATA ·jsonQuoteEscapes+224(SB)/8, $0x060063313030755c
DATA ·jsonQuoteEscapes+232(SB)/8, $0x060064313030755c
DATA ·jsonQuoteEscapes+240(SB)/8, $0x060065313030755c
DATA ·jsonQuoteEscapes+248(SB)/8, $0x060066313030755c
DATA ·jsonQuoteEscapes+272(SB)/8, $0x020000000000225c
DATA ·jsonQuoteEscapes+304(SB)/8, $0x060036323030755c
DATA ·jsonQuoteEscapes+480(SB)/8, $0x060063333030755c
DATA ·jsonQuoteEscapes+496(SB)/8, $0x060065333030755c
DATA ·jsonQuoteEscapes+736(SB)/8, $0x0200000000005c5c
GLOBL ·jsonQuoteEscapes(SB), RODATA|NOPTR, $1024
