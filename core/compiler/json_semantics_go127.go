//go:build go1.27

package compiler

// Go 1.27 enables encoding/json/v2 by default, which permits floating-point
// map keys and clears non-empty interfaces when a decode target is invalid.
const jitJSONV2Semantics = true
