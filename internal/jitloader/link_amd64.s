//go:build go1.24 && !go1.26

#include "textflag.h"

TEXT ·registerModuleABI(SB), NOSPLIT, $0-8
	MOVQ	mod+0(FP), DI
	CALL	runtime·addmoduledata(SB)
	RET
