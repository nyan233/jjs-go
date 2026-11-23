package compiler

import (
	"unicode"
	"unicode/utf8"
)

// Canonicalize a field name using the same equivalence classes as EqualFold.
// Uppercasing alone is insufficient for Unicode's multi-member fold cycles.
func appendFoldedField(dst []byte, name string) []byte {
	for len(name) != 0 {
		if c := name[0]; c < utf8.RuneSelf {
			if c >= 'a' && c <= 'z' {
				c -= 'a' - 'A'
			}
			dst = append(dst, c)
			name = name[1:]
			continue
		}
		r, size := utf8.DecodeRuneInString(name)
		name = name[size:]
		for {
			next := unicode.SimpleFold(r)
			if next <= r {
				r = next
				break
			}
			r = next
		}
		dst = utf8.AppendRune(dst, r)
	}
	return dst
}
