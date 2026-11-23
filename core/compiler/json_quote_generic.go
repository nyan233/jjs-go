//go:build !arm64

package compiler

func appendQuoted(dst []byte, value string) []byte { return appendQuotedGo(dst, value) }
