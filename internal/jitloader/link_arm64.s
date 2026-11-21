//go:build go1.24 && !go1.26

#include "textflag.h"

TEXT ·registerModuleABI(SB), NOSPLIT, $0-8
	MOVD	mod+0(FP), R0
	CALL	runtime·addmoduledata(SB)
	RET
