package helper

import (
	"encoding/hex"
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestHelper(t *testing.T) {
	buf := Int2Bytes[int64](0x912089537534)
	t.Log(hex.EncodeToString(buf))
	assert.Equal(t, hex.EncodeToString(buf), "3475538920910000", "output result not eq")
}
