package sys

import "testing"

func TestBytesAlloc(t *testing.T) {
	buf := AABytes(12)
	ModifyPermissions2Exec(buf)
	t.Log(len(buf))
	t.Log(cap(buf))
}
