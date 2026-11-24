#include "textflag.h"

// RDI = destination, RSI = source, CX = length, R8 = alphabet.
// The vector loop consumes exactly 12 input bytes and writes 16 bytes.
// Scalar code handles the final 0-11 bytes without reading past the source.
// The sextet packing layout follows CloudWeGo/base64x's Apache-2.0 AVX2
// implementation, expressed here with bounded exact loads and a scalar tail.
TEXT ·base64EncodeAVX2(SB), NOSPLIT, $0-32
	MOVQ dst+0(FP), DI
	MOVQ src+8(FP), SI
	MOVQ length+16(FP), CX
	MOVQ alphabet+24(FP), R8

	CMPQ CX, $12
	JL   scalar

	LEAQ base64Offsets<>(SB), R9
	VMOVDQU (R9), X0
	LEAQ base64Shuffle<>(SB), R9
	VMOVDQU (R9), X1
	LEAQ base64MaskFC<>(SB), R9
	VMOVDQU (R9), X2
	LEAQ base64Mul<>(SB), R9
	VMOVDQU (R9), X3
	LEAQ base64Mask3F<>(SB), R9
	VMOVDQU (R9), X4
	MOVL $26, R9
	MOVD R9, X11
	VPBROADCASTB X11, X5
	MOVL $51, R9
	MOVD R9, X11
	VPBROADCASTB X11, X6
	MOVL $13, R9
	MOVD R9, X11
	VPBROADCASTB X11, X7

vector:
	MOVQ (SI), X8
	MOVL 8(SI), R9
	PINSRD $2, R9, X8
	VPSHUFB X1, X8, X8

	VPAND X2, X8, X9
	VPMULHUW X3, X9, X9
	VPAND X4, X8, X8
	VPSLLW $8, X8, X10
	VPSLLW $4, X8, X8
	VPBLENDW $0xaa, X10, X8, X8
	VPOR X9, X8, X8

	VPCMPGTB X8, X5, X9
	VPAND X7, X9, X9
	VPSUBUSB X6, X8, X10
	VPOR X10, X9, X9
	VPSHUFB X9, X0, X9
	VPADDB X8, X9, X8

	VMOVDQU X8, (DI)
	ADDQ $12, SI
	ADDQ $16, DI
	SUBQ $12, CX
	CMPQ CX, $12
	JGE  vector

scalar:
	CMPQ CX, $3
	JL   tail
triple:
	MOVBLZX (SI), R9
	MOVBLZX 1(SI), R10
	MOVBLZX 2(SI), R11
	MOVQ R9, R12
	SHRQ $2, R12
	MOVBLZX (R8)(R12*1), R12
	MOVB R12, (DI)
	SHLQ $4, R9
	MOVQ R10, R12
	SHRQ $4, R12
	ORQ R12, R9
	ANDQ $63, R9
	MOVBLZX (R8)(R9*1), R9
	MOVB R9, 1(DI)
	SHLQ $2, R10
	MOVQ R11, R12
	SHRQ $6, R12
	ORQ R12, R10
	ANDQ $63, R10
	MOVBLZX (R8)(R10*1), R10
	MOVB R10, 2(DI)
	ANDQ $63, R11
	MOVBLZX (R8)(R11*1), R11
	MOVB R11, 3(DI)
	ADDQ $3, SI
	ADDQ $4, DI
	SUBQ $3, CX
	CMPQ CX, $3
	JGE  triple

tail:
	TESTQ CX, CX
	JZ    end
	MOVBLZX (SI), R9
	MOVQ R9, R10
	SHRQ $2, R10
	MOVBLZX (R8)(R10*1), R10
	MOVB R10, (DI)
	SHLQ $4, R9
	ANDQ $63, R9
	CMPQ CX, $1
	JE   one
	MOVBLZX 1(SI), R11
	MOVQ R11, R10
	SHRQ $4, R10
	ORQ R10, R9
	MOVBLZX (R8)(R9*1), R9
	MOVB R9, 1(DI)
	SHLQ $2, R11
	ANDQ $63, R11
	MOVBLZX (R8)(R11*1), R11
	MOVB R11, 2(DI)
	MOVB $61, 3(DI)
	JMP  end
one:
	MOVBLZX (R8)(R9*1), R9
	MOVB R9, 1(DI)
	MOVB $61, 2(DI)
	MOVB $61, 3(DI)

end:
	VZEROUPPER
	RET

DATA base64Offsets<>+0(SB)/8, $0xfcfcfcfcfcfcfc47
DATA base64Offsets<>+8(SB)/8, $0x000041f0edfcfcfc
DATA base64Offsets<>+16(SB)/8, $0xfcfcfcfcfcfcfc47
DATA base64Offsets<>+24(SB)/8, $0x000041f0edfcfcfc
GLOBL base64Offsets<>(SB), RODATA|NOPTR, $32

DATA base64Shuffle<>+0(SB)/8, $0x0405030401020001
DATA base64Shuffle<>+8(SB)/8, $0x0a0b090a07080607
DATA base64Shuffle<>+16(SB)/8, $0x0405030401020001
DATA base64Shuffle<>+24(SB)/8, $0x0a0b090a07080607
GLOBL base64Shuffle<>(SB), RODATA|NOPTR, $32

DATA base64MaskFC<>+0(SB)/8, $0x0fc0fc000fc0fc00
DATA base64MaskFC<>+8(SB)/8, $0x0fc0fc000fc0fc00
DATA base64MaskFC<>+16(SB)/8, $0x0fc0fc000fc0fc00
DATA base64MaskFC<>+24(SB)/8, $0x0fc0fc000fc0fc00
GLOBL base64MaskFC<>(SB), RODATA|NOPTR, $32

DATA base64Mul<>+0(SB)/8, $0x0400004004000040
DATA base64Mul<>+8(SB)/8, $0x0400004004000040
DATA base64Mul<>+16(SB)/8, $0x0400004004000040
DATA base64Mul<>+24(SB)/8, $0x0400004004000040
GLOBL base64Mul<>(SB), RODATA|NOPTR, $32

DATA base64Mask3F<>+0(SB)/8, $0x003f03f0003f03f0
DATA base64Mask3F<>+8(SB)/8, $0x003f03f0003f03f0
DATA base64Mask3F<>+16(SB)/8, $0x003f03f0003f03f0
DATA base64Mask3F<>+24(SB)/8, $0x003f03f0003f03f0
GLOBL base64Mask3F<>(SB), RODATA|NOPTR, $32
