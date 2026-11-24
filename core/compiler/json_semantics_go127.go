//go:build go1.27

package compiler

// Go 1.27 enables encoding/json/v2 by default. Its map-key rules take
// precedence over string and integer defaults and permit floating-point keys.
const jitJSONV2Semantics = true
