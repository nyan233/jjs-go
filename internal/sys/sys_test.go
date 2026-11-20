package sys

import (
	"testing"
	"unsafe"
)

func TestBytesAlloc(t *testing.T) {
	buf := AABytes(12)
	SetExecPermissions(unsafe.Pointer(&buf[0]), uintptr(cap(buf)))
	buf[0] = 0x90
	if buf[0] != 0x90 {
		t.Fatal("executable page is not writable after SetExecPermissions")
	}
	if len(buf) != 12 || cap(buf) < 12 {
		t.Fatalf("unexpected allocation: len=%d cap=%d", len(buf), cap(buf))
	}

	code := []byte{0xc3}
	executable := ModifyPermissions2Exec(code)
	if len(executable) != len(code) || executable[0] != 0xc3 {
		t.Fatalf("unexpected executable page: %v", executable)
	}
}
