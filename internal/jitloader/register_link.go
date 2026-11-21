//go:build (go1.21 && !go1.24) || go1.26

package jitloader

import (
	"sync/atomic"
	"unsafe"
)

//go:linkname lastModuleData runtime.lastmoduledatap
var lastModuleData *moduledata

func registerModule(mod *moduledata) {
	for {
		tail := lastModuleData
		if atomic.CompareAndSwapPointer(
			(*unsafe.Pointer)(unsafe.Pointer(&lastModuleData)),
			unsafe.Pointer(tail),
			unsafe.Pointer(mod),
		) {
			atomic.StorePointer(
				(*unsafe.Pointer)(unsafe.Pointer(&tail.next)),
				unsafe.Pointer(mod),
			)
			return
		}
	}
}
