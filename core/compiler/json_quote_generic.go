//go:build !arm64 && (!amd64 || arm64_asm_test)

package compiler

func appendQuoted(dst []byte, value string) []byte { return appendQuotedGo(dst, value) }

func initJITQuoteHelpers(program *jitEncodeProgram) {}
