//go:build !go1.26

package compiler

func runtimeDirectInterface(header *runtimeTypeHeader) bool {
	return header.Kind&runtimeDirectIface != 0
}
