//go:build arm64 || arm64_asm_test

package compiler

import (
	"bytes"
	"testing"
)

func TestARM64JSONScanners(t *testing.T) {
	escaping := []byte{0, '"', '\\', '<', '>', '&', 0x1f, 0x80, 0xff}
	stringEnds := []byte{0, '"', '\\', 0x1f, 0x80, 0xff}
	for length := 0; length <= 300; length++ {
		safe := bytes.Repeat([]byte{'a'}, length)
		if got := scanJSONEscape((*byte)(unsafeSliceData(safe)), len(safe)); got != -1 {
			t.Fatalf("escape length %d: got %d", length, got)
		}
		if got := scanJSONStringEnd((*byte)(unsafeSliceData(safe)), len(safe)); got != -1 {
			t.Fatalf("string end length %d: got %d", length, got)
		}
		for position := 0; position < length; position++ {
			for _, special := range escaping {
				value := bytes.Clone(safe)
				value[position] = special
				if got := scanJSONEscape((*byte)(unsafeSliceData(value)), len(value)); got != position {
					t.Fatalf("escape length %d position %d byte %#x: got %d", length, position, special, got)
				}
			}
		}
		for position := 0; position < length; position++ {
			for _, special := range stringEnds {
				value := bytes.Clone(safe)
				value[position] = special
				if got := scanJSONStringEnd((*byte)(unsafeSliceData(value)), len(value)); got != position {
					t.Fatalf("string end length %d position %d byte %#x: got %d", length, position, special, got)
				}
			}
		}
	}
}

func unsafeSliceData(value []byte) *byte {
	if len(value) == 0 {
		return nil
	}
	return &value[0]
}
