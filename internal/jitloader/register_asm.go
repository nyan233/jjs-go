//go:build go1.24 && !go1.26

package jitloader

func registerModule(mod *moduledata) {
	registerModuleABI(mod)
}
