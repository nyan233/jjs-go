package compiler

import (
	"unicode/utf8"
	"unsafe"
)

func splitJSONTag(tag string) (name, options string) {
	if tag == "" {
		return "", ""
	}
	name, options, _ = cutString(tag, ",")
	return
}

func cutString(s, sep string) (before, after string, found bool) {
	for i := 0; i+len(sep) <= len(s); i++ {
		if s[i:i+len(sep)] == sep {
			return s[:i], s[i+len(sep):], true
		}
	}
	return s, "", false
}

func isValidJSONTagName(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		if r == ',' || r == '\\' || r == '"' || r <= ' ' || r == utf8.RuneError {
			return false
		}
	}
	return true
}

func appendQuoted(dst []byte, value string) []byte {
	const hex = "0123456789abcdef"
	dst = append(dst, '"')
	start, scanAt := 0, 0
	for i := 0; i < len(value); {
		if remaining := len(value) - i; i >= scanAt && value[i] < utf8.RuneSelf && remaining >= jitEncodeShortScanBytes {
			ptr := (*byte)(unsafe.Add(unsafe.Pointer(unsafe.StringData(value)), i))
			special := scanJSONEscape(ptr, remaining)
			if special < 0 {
				break
			}
			i += special
			// Dense escapes are cheaper to handle scalarly. Resume bulk scans
			// after a short interval instead of retrying at every special byte.
			scanAt = i + 16
		}
		c := value[i]
		if c < utf8.RuneSelf {
			if c >= 0x20 && c != '\\' && c != '"' && c != '<' && c != '>' && c != '&' {
				i++
				continue
			}
			dst = append(dst, value[start:i]...)
			switch c {
			case '\\', '"':
				dst = append(dst, '\\', c)
			case '\b':
				dst = append(dst, '\\', 'b')
			case '\f':
				dst = append(dst, '\\', 'f')
			case '\n':
				dst = append(dst, '\\', 'n')
			case '\r':
				dst = append(dst, '\\', 'r')
			case '\t':
				dst = append(dst, '\\', 't')
			case '<':
				dst = append(dst, '\\', 'u', '0', '0', '3', 'c')
			case '>':
				dst = append(dst, '\\', 'u', '0', '0', '3', 'e')
			case '&':
				dst = append(dst, '\\', 'u', '0', '0', '2', '6')
			default:
				dst = append(dst, '\\', 'u', '0', '0', hex[c>>4], hex[c&0x0f])
			}
			i++
			start = i
			continue
		}

		r, size := utf8.DecodeRuneInString(value[i:])
		if r == utf8.RuneError && size == 1 {
			dst = append(dst, value[start:i]...)
			dst = append(dst, '\\', 'u', 'f', 'f', 'f', 'd')
			i++
			start = i
			continue
		}
		if r == '\u2028' || r == '\u2029' {
			dst = append(dst, value[start:i]...)
			dst = append(dst, '\\', 'u', '2', '0', '2', byte('8'+r-'\u2028'))
			start = i + size
		}
		i += size
	}
	dst = append(dst, value[start:]...)
	return append(dst, '"')
}
