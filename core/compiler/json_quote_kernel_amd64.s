//go:build amd64 && !arm64_asm_test

#include "textflag.h"

// Bounded fused scan/copy/escape. No calls, stack changes or allocation.
// DI/RBX: output cursor/end; SI/R8: input cursor/end.
// R9/R10: original output/input starts; R11: chunk end; R15: scalar-window end.
TEXT ·quoteJSONString(SB), NOSPLIT, $0-48
	MOVQ dst+0(FP), DI
	MOVQ capacity+8(FP), BX
	MOVQ src+16(FP), SI
	MOVQ length+24(FP), R8
	MOVQ DI, R9
	MOVQ SI, R10
	ADDQ DI, BX
	ADDQ SI, R8
	MOVQ R8, R11
	SUBQ R10, R11
	CMPQ R11, $65536
	JLE  chunk_ready
	MOVQ $65536, R11
chunk_ready:
	ADDQ R10, R11
	LEAQ ·jsonQuoteEscapesAMD64(SB), R12
	XORQ R15, R15

	MOVQ R8, AX
	SUBQ SI, AX
	CMPQ AX, $8
	JL   quote_scalar_tail

	LEAQ ·jsonQuoteQuoteAMD64(SB), AX
	VMOVDQU (AX), Y1
	LEAQ ·jsonQuoteSlashAMD64(SB), AX
	VMOVDQU (AX), Y2
	LEAQ ·jsonQuoteLessAMD64(SB), AX
	VMOVDQU (AX), Y3
	LEAQ ·jsonQuoteGreaterAMD64(SB), AX
	VMOVDQU (AX), Y4
	LEAQ ·jsonQuoteAmpAMD64(SB), AX
	VMOVDQU (AX), Y5
	LEAQ ·jsonQuoteControlAMD64(SB), AX
	VMOVDQU (AX), Y6

quote_loop:
	MOVQ R11, CX
	SUBQ SI, CX
	TESTQ CX, CX
	JLE  quote_done
	MOVQ BX, DX
	SUBQ DI, DX
	TESTQ DX, DX
	JLE  quote_done
	CMPQ CX, $32
	JL   quote_try16
	CMPQ DX, $32
	JL   quote_scalar

	VMOVDQU (SI), Y0
	VPCMPEQB Y1, Y0, Y7
	VPMOVMSKB Y7, R13
	VPCMPEQB Y2, Y0, Y7
	VPMOVMSKB Y7, AX
	ORQ AX, R13
	VPCMPEQB Y3, Y0, Y7
	VPMOVMSKB Y7, AX
	ORQ AX, R13
	VPCMPEQB Y4, Y0, Y7
	VPMOVMSKB Y7, AX
	ORQ AX, R13
	VPCMPEQB Y5, Y0, Y7
	VPMOVMSKB Y7, AX
	ORQ AX, R13
	VPMINUB Y6, Y0, Y7
	VPCMPEQB Y7, Y0, Y7
	VPMOVMSKB Y7, AX
	ORQ AX, R13
	VPMOVMSKB Y0, AX
	ORQ AX, R13
	TESTQ R13, R13
	JNZ  quote_special32
	VMOVDQU Y0, (DI)
	ADDQ $32, SI
	ADDQ $32, DI
	JMP  quote_loop

quote_try16:
	CMPQ CX, $16
	JL   quote_try8
	CMPQ DX, $16
	JL   quote_scalar
	VMOVDQU (SI), X0
	VPCMPEQB X1, X0, X7
	VPMOVMSKB X7, R13
	VPCMPEQB X2, X0, X7
	VPMOVMSKB X7, AX
	ORQ AX, R13
	VPCMPEQB X3, X0, X7
	VPMOVMSKB X7, AX
	ORQ AX, R13
	VPCMPEQB X4, X0, X7
	VPMOVMSKB X7, AX
	ORQ AX, R13
	VPCMPEQB X5, X0, X7
	VPMOVMSKB X7, AX
	ORQ AX, R13
	VPMINUB X6, X0, X7
	VPCMPEQB X7, X0, X7
	VPMOVMSKB X7, AX
	ORQ AX, R13
	VPMOVMSKB X0, AX
	ORQ AX, R13
	TESTQ R13, R13
	JNZ  quote_special16
	VMOVDQU X0, (DI)
	ADDQ $16, SI
	ADDQ $16, DI
	JMP  quote_loop

quote_try8:
	CMPQ CX, $8
	JL   quote_scalar
	CMPQ DX, $8
	JL   quote_scalar
	VMOVQ (SI), X0
	VPCMPEQB X1, X0, X7
	VPMOVMSKB X7, R13
	VPCMPEQB X2, X0, X7
	VPMOVMSKB X7, AX
	ORQ AX, R13
	VPCMPEQB X3, X0, X7
	VPMOVMSKB X7, AX
	ORQ AX, R13
	VPCMPEQB X4, X0, X7
	VPMOVMSKB X7, AX
	ORQ AX, R13
	VPCMPEQB X5, X0, X7
	VPMOVMSKB X7, AX
	ORQ AX, R13
	VPMINUB X6, X0, X7
	VPCMPEQB X7, X0, X7
	VPMOVMSKB X7, AX
	ORQ AX, R13
	VPMOVMSKB X0, AX
	ORQ AX, R13
	ANDQ $0xff, R13
	TESTQ R13, R13
	JNZ  quote_special8
	VMOVQ X0, (DI)
	ADDQ $8, SI
	ADDQ $8, DI
	JMP  quote_loop

quote_special32:
	BSFQ R13, R13
	MOVQ R13, CX
	JMP  quote_copy_prefix
quote_special16:
	BSFQ R13, R13
	MOVQ R13, CX
	JMP  quote_copy_prefix
quote_special8:
	BSFQ R13, R13
	MOVQ R13, CX
quote_copy_prefix:
	TESTQ CX, CX
	JZ   quote_prefix_done
quote_prefix_loop:
	MOVB (SI), AX
	MOVB AX, (DI)
	INCQ SI
	INCQ DI
	DECQ CX
	JNZ  quote_prefix_loop
quote_prefix_done:
	MOVQ R11, R15
	SUBQ SI, R15
	CMPQ R15, $16
	JLE  quote_window_ready
	MOVQ $16, R15
quote_window_ready:
	ADDQ SI, R15
	JMP  quote_scalar

quote_scalar_tail:
	MOVQ R11, R15
quote_scalar:
	TESTQ R15, R15
	JNZ  quote_scalar_loop
	MOVQ R11, R15
	SUBQ SI, R15
	CMPQ R15, $16
	JLE  quote_scalar_window
	MOVQ $16, R15
quote_scalar_window:
	ADDQ SI, R15
quote_scalar_loop:
	MOVQ R11, CX
	SUBQ SI, CX
	TESTQ CX, CX
	JLE  quote_done
	MOVQ BX, DX
	SUBQ DI, DX
	TESTQ DX, DX
	JLE  quote_done
	MOVBLZX (SI), AX
	CMPQ AX, $0x80
	JAE  quote_utf8
	MOVQ (R12)(AX*8), R13
	TESTQ R13, R13
	JNZ  quote_ascii_escape
	MOVB AX, (DI)
	INCQ SI
	INCQ DI
	JMP  quote_scalar_progress

quote_ascii_escape:
	MOVQ R13, CX
	SHRQ $56, CX
	MOVQ BX, DX
	SUBQ DI, DX
	CMPQ DX, CX
	JB   quote_done
	CMPQ CX, $2
	JE   quote_escape_two
	MOVL R13, (DI)
	MOVQ R13, DX
	SHRQ $32, DX
	MOVW DX, 4(DI)
	ADDQ $6, DI
	INCQ SI
	JMP  quote_scalar_progress
quote_escape_two:
	MOVW R13, (DI)
	ADDQ $2, DI
	INCQ SI
	JMP  quote_scalar_progress

quote_utf8:
	CMPQ AX, $0xc2
	JB   quote_invalid
	CMPQ AX, $0xe0
	JB   quote_utf8_two
	CMPQ AX, $0xf0
	JB   quote_utf8_three
	CMPQ AX, $0xf5
	JAE  quote_invalid

	MOVQ R8, CX
	SUBQ SI, CX
	CMPQ CX, $4
	JL   quote_invalid
	MOVBLZX 1(SI), DX
	MOVQ DX, R13
	SUBL $0x80, R13
	CMPQ R13, $0x40
	JAE  quote_invalid
	CMPQ AX, $0xf0
	JNE  quote_utf8_four_max
	CMPQ DX, $0x90
	JB   quote_invalid
quote_utf8_four_max:
	CMPQ AX, $0xf4
	JNE  quote_utf8_four_tail
	CMPQ DX, $0x90
	JAE  quote_invalid
quote_utf8_four_tail:
	MOVBLZX 2(SI), DX
	MOVQ DX, R13
	SUBL $0x80, R13
	CMPQ R13, $0x40
	JAE  quote_invalid
	MOVBLZX 3(SI), DX
	MOVQ DX, R13
	SUBL $0x80, R13
	CMPQ R13, $0x40
	JAE  quote_invalid
	MOVQ BX, CX
	SUBQ DI, CX
	CMPQ CX, $4
	JB   quote_done
	MOVL (SI), DX
	MOVL DX, (DI)
	ADDQ $4, SI
	ADDQ $4, DI
	JMP  quote_scalar_progress

quote_utf8_two:
	MOVQ R8, CX
	SUBQ SI, CX
	CMPQ CX, $2
	JL   quote_invalid
	MOVBLZX 1(SI), DX
	MOVQ DX, R13
	SUBL $0x80, R13
	CMPQ R13, $0x40
	JAE  quote_invalid
	MOVQ BX, CX
	SUBQ DI, CX
	CMPQ CX, $2
	JB   quote_done
	MOVW (SI), DX
	MOVW DX, (DI)
	ADDQ $2, SI
	ADDQ $2, DI
	JMP  quote_scalar_progress

quote_utf8_three:
	MOVQ R8, CX
	SUBQ SI, CX
	CMPQ CX, $3
	JL   quote_invalid
	MOVBLZX 1(SI), DX
	MOVQ DX, R13
	SUBL $0x80, R13
	CMPQ R13, $0x40
	JAE  quote_invalid
	CMPQ AX, $0xe0
	JNE  quote_utf8_three_max
	CMPQ DX, $0xa0
	JB   quote_invalid
quote_utf8_three_max:
	CMPQ AX, $0xed
	JNE  quote_utf8_three_tail
	CMPQ DX, $0xa0
	JAE  quote_invalid
quote_utf8_three_tail:
	MOVBLZX 2(SI), R13
	MOVQ R13, CX
	SUBL $0x80, CX
	CMPQ CX, $0x40
	JAE  quote_invalid
	CMPQ AX, $0xe2
	JNE  quote_copy_three
	CMPQ DX, $0x80
	JNE  quote_copy_three
	MOVQ R13, CX
	ANDQ $0xfe, CX
	CMPQ CX, $0xa8
	JE   quote_separator
quote_copy_three:
	MOVQ BX, CX
	SUBQ DI, CX
	CMPQ CX, $3
	JB   quote_done
	MOVW (SI), DX
	MOVW DX, (DI)
	MOVB R13, 2(DI)
	ADDQ $3, SI
	ADDQ $3, DI
	JMP  quote_scalar_progress

quote_separator:
	MOVQ BX, CX
	SUBQ DI, CX
	CMPQ CX, $6
	JB   quote_done
	MOVL $0x3032755c, (DI)
	MOVQ R13, DX
	ANDQ $1, DX
	SHLQ $8, DX
	ADDQ $0x3832, DX
	MOVW DX, 4(DI)
	ADDQ $6, DI
	ADDQ $3, SI
	JMP  quote_scalar_progress

quote_invalid:
	MOVQ BX, CX
	SUBQ DI, CX
	CMPQ CX, $6
	JB   quote_done
	MOVL $0x6666755c, (DI)
	MOVW $0x6466, 4(DI)
	ADDQ $6, DI
	INCQ SI

quote_scalar_progress:
	MOVQ R15, CX
	SUBQ SI, CX
	TESTQ CX, CX
	JG   quote_scalar_loop
	XORQ R15, R15
	JMP  quote_loop

quote_done:
	MOVQ SI, AX
	SUBQ R10, AX
	MOVQ AX, consumed+32(FP)
	MOVQ DI, AX
	SUBQ R9, AX
	MOVQ AX, written+40(FP)
	VZEROUPPER
	RET

// Packed escape bytes, with output length in the top byte; zero means safe.
DATA ·jsonQuoteEscapesAMD64+0(SB)/8, $0x060030303030755c
DATA ·jsonQuoteEscapesAMD64+8(SB)/8, $0x060031303030755c
DATA ·jsonQuoteEscapesAMD64+16(SB)/8, $0x060032303030755c
DATA ·jsonQuoteEscapesAMD64+24(SB)/8, $0x060033303030755c
DATA ·jsonQuoteEscapesAMD64+32(SB)/8, $0x060034303030755c
DATA ·jsonQuoteEscapesAMD64+40(SB)/8, $0x060035303030755c
DATA ·jsonQuoteEscapesAMD64+48(SB)/8, $0x060036303030755c
DATA ·jsonQuoteEscapesAMD64+56(SB)/8, $0x060037303030755c
DATA ·jsonQuoteEscapesAMD64+64(SB)/8, $0x020000000000625c
DATA ·jsonQuoteEscapesAMD64+72(SB)/8, $0x020000000000745c
DATA ·jsonQuoteEscapesAMD64+80(SB)/8, $0x0200000000006e5c
DATA ·jsonQuoteEscapesAMD64+88(SB)/8, $0x060062303030755c
DATA ·jsonQuoteEscapesAMD64+96(SB)/8, $0x020000000000665c
DATA ·jsonQuoteEscapesAMD64+104(SB)/8, $0x020000000000725c
DATA ·jsonQuoteEscapesAMD64+112(SB)/8, $0x060065303030755c
DATA ·jsonQuoteEscapesAMD64+120(SB)/8, $0x060066303030755c
DATA ·jsonQuoteEscapesAMD64+128(SB)/8, $0x060030313030755c
DATA ·jsonQuoteEscapesAMD64+136(SB)/8, $0x060031313030755c
DATA ·jsonQuoteEscapesAMD64+144(SB)/8, $0x060032313030755c
DATA ·jsonQuoteEscapesAMD64+152(SB)/8, $0x060033313030755c
DATA ·jsonQuoteEscapesAMD64+160(SB)/8, $0x060034313030755c
DATA ·jsonQuoteEscapesAMD64+168(SB)/8, $0x060035313030755c
DATA ·jsonQuoteEscapesAMD64+176(SB)/8, $0x060036313030755c
DATA ·jsonQuoteEscapesAMD64+184(SB)/8, $0x060037313030755c
DATA ·jsonQuoteEscapesAMD64+192(SB)/8, $0x060038313030755c
DATA ·jsonQuoteEscapesAMD64+200(SB)/8, $0x060039313030755c
DATA ·jsonQuoteEscapesAMD64+208(SB)/8, $0x060061313030755c
DATA ·jsonQuoteEscapesAMD64+216(SB)/8, $0x060062313030755c
DATA ·jsonQuoteEscapesAMD64+224(SB)/8, $0x060063313030755c
DATA ·jsonQuoteEscapesAMD64+232(SB)/8, $0x060064313030755c
DATA ·jsonQuoteEscapesAMD64+240(SB)/8, $0x060065313030755c
DATA ·jsonQuoteEscapesAMD64+248(SB)/8, $0x060066313030755c
DATA ·jsonQuoteEscapesAMD64+272(SB)/8, $0x020000000000225c
DATA ·jsonQuoteEscapesAMD64+304(SB)/8, $0x060036323030755c
DATA ·jsonQuoteEscapesAMD64+480(SB)/8, $0x060063333030755c
DATA ·jsonQuoteEscapesAMD64+496(SB)/8, $0x060065333030755c
DATA ·jsonQuoteEscapesAMD64+736(SB)/8, $0x0200000000005c5c
GLOBL ·jsonQuoteEscapesAMD64(SB), RODATA|NOPTR, $1024

DATA ·jsonQuoteQuoteAMD64+0(SB)/8, $0x2222222222222222
DATA ·jsonQuoteQuoteAMD64+8(SB)/8, $0x2222222222222222
DATA ·jsonQuoteQuoteAMD64+16(SB)/8, $0x2222222222222222
DATA ·jsonQuoteQuoteAMD64+24(SB)/8, $0x2222222222222222
GLOBL ·jsonQuoteQuoteAMD64(SB), RODATA|NOPTR, $32

DATA ·jsonQuoteSlashAMD64+0(SB)/8, $0x5c5c5c5c5c5c5c5c
DATA ·jsonQuoteSlashAMD64+8(SB)/8, $0x5c5c5c5c5c5c5c5c
DATA ·jsonQuoteSlashAMD64+16(SB)/8, $0x5c5c5c5c5c5c5c5c
DATA ·jsonQuoteSlashAMD64+24(SB)/8, $0x5c5c5c5c5c5c5c5c
GLOBL ·jsonQuoteSlashAMD64(SB), RODATA|NOPTR, $32

DATA ·jsonQuoteLessAMD64+0(SB)/8, $0x3c3c3c3c3c3c3c3c
DATA ·jsonQuoteLessAMD64+8(SB)/8, $0x3c3c3c3c3c3c3c3c
DATA ·jsonQuoteLessAMD64+16(SB)/8, $0x3c3c3c3c3c3c3c3c
DATA ·jsonQuoteLessAMD64+24(SB)/8, $0x3c3c3c3c3c3c3c3c
GLOBL ·jsonQuoteLessAMD64(SB), RODATA|NOPTR, $32

DATA ·jsonQuoteGreaterAMD64+0(SB)/8, $0x3e3e3e3e3e3e3e3e
DATA ·jsonQuoteGreaterAMD64+8(SB)/8, $0x3e3e3e3e3e3e3e3e
DATA ·jsonQuoteGreaterAMD64+16(SB)/8, $0x3e3e3e3e3e3e3e3e
DATA ·jsonQuoteGreaterAMD64+24(SB)/8, $0x3e3e3e3e3e3e3e3e
GLOBL ·jsonQuoteGreaterAMD64(SB), RODATA|NOPTR, $32

DATA ·jsonQuoteAmpAMD64+0(SB)/8, $0x2626262626262626
DATA ·jsonQuoteAmpAMD64+8(SB)/8, $0x2626262626262626
DATA ·jsonQuoteAmpAMD64+16(SB)/8, $0x2626262626262626
DATA ·jsonQuoteAmpAMD64+24(SB)/8, $0x2626262626262626
GLOBL ·jsonQuoteAmpAMD64(SB), RODATA|NOPTR, $32

DATA ·jsonQuoteControlAMD64+0(SB)/8, $0x1f1f1f1f1f1f1f1f
DATA ·jsonQuoteControlAMD64+8(SB)/8, $0x1f1f1f1f1f1f1f1f
DATA ·jsonQuoteControlAMD64+16(SB)/8, $0x1f1f1f1f1f1f1f1f
DATA ·jsonQuoteControlAMD64+24(SB)/8, $0x1f1f1f1f1f1f1f1f
GLOBL ·jsonQuoteControlAMD64(SB), RODATA|NOPTR, $32
