package compiler

import (
	"math/bits"
	"unsafe"
)

// skipString validates JSON string syntax without constructing its value.
// Invalid UTF-8 and unpaired surrogates are accepted, as in parseString: their
// replacement characters are immaterial when the decoded value is discarded.
func (p *jitJSONParser) skipString() error {
	if p.index >= len(p.data) || p.data[p.index] != '"' {
		return p.errorf("expected string")
	}
	index, data := p.index+1, p.data
	for index < len(data) {
		// Skipping does not need UTF-8 normalization. Scan eight bytes at a
		// time, stopping only for string syntax, including in non-ASCII text.
		for len(data)-index >= 8 {
			word := *(*uint64)(unsafe.Pointer(&data[index]))
			const ones, high = uint64(0x0101010101010101), uint64(0x8080808080808080)
			quotes := word ^ 0x2222222222222222
			slashes := word ^ 0x5c5c5c5c5c5c5c5c
			mask := (quotes-ones)&^quotes&high | (slashes-ones)&^slashes&high |
				(word-0x2020202020202020)&^word&high
			if mask != 0 {
				index += bits.TrailingZeros64(mask) / 8
				break
			}
			index += 8
		}
		if index == len(data) {
			break
		}
		c := data[index]
		index++
		switch {
		case c == '"':
			p.index = index
			return nil
		case c == '\\':
			if index == len(data) {
				p.index = index
				return p.errorf("unexpected end of string escape")
			}
			escape := data[index]
			index++
			switch escape {
			case '"', '\\', '/', 'b', 'f', 'n', 'r', 't':
			case 'u':
				if len(data)-index < 4 {
					p.index = index
					return p.errorf("short unicode escape")
				}
				for end := index + 4; index < end; index++ {
					hex := data[index]
					if !('0' <= hex && hex <= '9' || 'a' <= hex && hex <= 'f' || 'A' <= hex && hex <= 'F') {
						p.index = index
						return p.errorf("invalid unicode escape")
					}
				}
			default:
				p.index = index
				return p.errorf("invalid string escape \\%c", escape)
			}
		case c < 0x20:
			p.index = index - 1
			return p.errorf("invalid control character in string")
		}
	}
	p.index = index
	return p.errorf("unexpected end of string")
}
