package compiler

import (
	"runtime"
	"strconv"
	"strings"
)

// Go 1.27.0 initially called TextMarshaler before the default string-key rule.
// Go 1.27.4 restored the documented string-first precedence.
var jitJSONV2TextMapKeysFirst = jitJSONV2Semantics && jitGo127TextMapKeysFirst(runtime.Version())

func jitGo127TextMapKeysFirst(version string) bool {
	index := strings.Index(version, "go1.27")
	if index < 0 {
		return false
	}
	rest := version[index+len("go1.27"):]
	if len(rest) == 0 || rest[0] != '.' {
		return false
	}
	end := 1
	for end < len(rest) && rest[end] >= '0' && rest[end] <= '9' {
		end++
	}
	if end == 1 {
		return false
	}
	patch, err := strconv.Atoi(rest[1:end])
	return err == nil && patch < 4
}
