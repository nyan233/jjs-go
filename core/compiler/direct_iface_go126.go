//go:build go1.26

package compiler

func runtimeDirectInterface(header *runtimeTypeHeader) bool {
	// Go 1.26 moved the direct-interface bit from Kind to TFlag.
	return header.Flags&runtimeDirectIface != 0
}
