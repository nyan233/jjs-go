package compiler

import (
	"fmt"
	"strconv"
	"unicode/utf16"
	"unicode/utf8"
	"unsafe"
)

type jitJSONKind uint8

const (
	jitJSONNull jitJSONKind = iota
	jitJSONBool
	jitJSONNumber
	jitJSONString
	jitJSONArray
	jitJSONObject
)

type jitJSONNode struct {
	kind    jitJSONKind
	boolean bool
	text    string
	array   []*jitJSONNode
	object  []jitJSONObjectField
}

type jitJSONObjectField struct {
	name  string
	value *jitJSONNode
}

type jitJSONParser struct {
	data  []byte
	index int
	depth int
}

// Escape decoding usually produces strings shorter than this. Keep the
// working buffer on the parser stack instead of allocating and growing it for
// every escaped string.
const jitDecodeStringScratchBytes = 512

func parseJITJSON(data []byte) (*jitJSONNode, error) {
	parser := jitJSONParser{data: data}
	parser.skipSpace()
	node, err := parser.parseValue()
	if err != nil {
		return nil, err
	}
	parser.skipSpace()
	if parser.index != len(parser.data) {
		return nil, parser.errorf("unexpected trailing data")
	}
	return node, nil
}

func (p *jitJSONParser) parseValue() (*jitJSONNode, error) {
	p.depth++
	if p.depth > 1000 {
		return nil, p.errorf("maximum JSON depth exceeded")
	}
	defer func() { p.depth-- }()
	p.skipSpace()
	if p.index >= len(p.data) {
		return nil, p.errorf("unexpected end of JSON input")
	}
	switch p.data[p.index] {
	case 'n':
		if !p.consumeLiteral("null") {
			return nil, p.errorf("invalid literal")
		}
		return &jitJSONNode{kind: jitJSONNull}, nil
	case 't':
		if !p.consumeLiteral("true") {
			return nil, p.errorf("invalid literal")
		}
		return &jitJSONNode{kind: jitJSONBool, boolean: true}, nil
	case 'f':
		if !p.consumeLiteral("false") {
			return nil, p.errorf("invalid literal")
		}
		return &jitJSONNode{kind: jitJSONBool}, nil
	case '"':
		value, err := p.parseString()
		if err != nil {
			return nil, err
		}
		return &jitJSONNode{kind: jitJSONString, text: value}, nil
	case '[':
		return p.parseArray()
	case '{':
		return p.parseObject()
	default:
		if p.data[p.index] == '-' || (p.data[p.index] >= '0' && p.data[p.index] <= '9') {
			value, err := p.parseNumber()
			if err != nil {
				return nil, err
			}
			return &jitJSONNode{kind: jitJSONNumber, text: value}, nil
		}
		return nil, p.errorf("invalid character %q", p.data[p.index])
	}
}

func (p *jitJSONParser) parseArray() (*jitJSONNode, error) {
	p.index++
	node := &jitJSONNode{kind: jitJSONArray}
	p.skipSpace()
	if p.consumeByte(']') {
		return node, nil
	}
	for {
		value, err := p.parseValue()
		if err != nil {
			return nil, err
		}
		node.array = append(node.array, value)
		p.skipSpace()
		if p.consumeByte(']') {
			return node, nil
		}
		if !p.consumeByte(',') {
			return nil, p.errorf("expected ',' or ']'")
		}
	}
}

func (p *jitJSONParser) parseObject() (*jitJSONNode, error) {
	p.index++
	node := &jitJSONNode{kind: jitJSONObject}
	p.skipSpace()
	if p.consumeByte('}') {
		return node, nil
	}
	for {
		p.skipSpace()
		if p.index >= len(p.data) || p.data[p.index] != '"' {
			return nil, p.errorf("expected object key")
		}
		name, err := p.parseString()
		if err != nil {
			return nil, err
		}
		p.skipSpace()
		if !p.consumeByte(':') {
			return nil, p.errorf("expected ':'")
		}
		value, err := p.parseValue()
		if err != nil {
			return nil, err
		}
		node.object = append(node.object, jitJSONObjectField{name: name, value: value})
		p.skipSpace()
		if p.consumeByte('}') {
			return node, nil
		}
		if !p.consumeByte(',') {
			return nil, p.errorf("expected ',' or '}'")
		}
	}
}

func (p *jitJSONParser) parseString() (string, error) {
	return p.parseStringInternal(true)
}

func (p *jitJSONParser) parseStringKey() (string, error) {
	return p.parseStringInternal(false)
}

func (p *jitJSONParser) parseStringInternal(copyValue bool) (string, error) {
	if p.index >= len(p.data) || p.data[p.index] != '"' {
		return "", p.errorf("expected string")
	}
	p.index++
	start := p.index
	scalarEnd := min(start+jitDecodeStringVectorBytes, len(p.data))

	for p.index < scalarEnd {
		c := p.data[p.index]
		switch {
		case c == '"':
			end := p.index
			p.index++
			return p.rawString(start, end, copyValue), nil
		case c == '\\':
			return p.parseStringSlow(start)
		case c < 0x20:
			return "", p.errorf("invalid control character in string")
		case c < utf8.RuneSelf:
			p.index++
		default:
			r, size := utf8.DecodeRune(p.data[p.index:])
			if r == utf8.RuneError && size == 1 {
				return p.parseStringSlow(start)
			}
			p.index += size
		}
	}

	for p.index < len(p.data) {
		if remaining := len(p.data) - p.index; p.data[p.index] < utf8.RuneSelf && remaining >= jitDecodeStringVectorBytes {
			ptr := (*byte)(unsafe.Add(unsafe.Pointer(unsafe.SliceData(p.data)), p.index))
			special := scanJSONStringEnd(ptr, remaining)
			if special < 0 {
				return "", p.errorf("unexpected end of string")
			}
			if special > 0 {
				p.index += special
				continue
			}
		}
		c := p.data[p.index]
		if c == '"' {
			end := p.index
			p.index++
			return p.rawString(start, end, copyValue), nil
		}
		if c == '\\' {
			return p.parseStringSlow(start)
		}
		if c < 0x20 {
			return "", p.errorf("invalid control character in string")
		}
		if c >= utf8.RuneSelf {
			r, size := utf8.DecodeRune(p.data[p.index:])
			if r == utf8.RuneError && size == 1 {
				return p.parseStringSlow(start)
			}
			p.index += size
			continue
		}
		p.index++
	}
	return "", p.errorf("unexpected end of string")
}

func (p *jitJSONParser) rawString(start, end int, copyValue bool) string {
	raw := p.data[start:end]
	// The scanner validated every non-ASCII rune before reaching this path.
	if !copyValue {
		return unsafe.String(unsafe.SliceData(raw), len(raw))
	}
	return string(raw)
}

func (p *jitJSONParser) parseStringSlow(start int) (string, error) {
	var scratch [jitDecodeStringScratchBytes]byte
	// Keep the already-validated prefix; resume at the first escape or invalid
	// UTF-8 byte instead of scanning the same prefix a second time.
	out := append(scratch[:0], p.data[start:p.index]...)
	for p.index < len(p.data) {
		c := p.data[p.index]
		if c == '"' {
			p.index++
			return string(out), nil
		}
		if c == '\\' {
			p.index++
			if p.index >= len(p.data) {
				return "", p.errorf("unexpected end of string escape")
			}
			escape := p.data[p.index]
			p.index++
			switch escape {
			case '"', '\\', '/':
				out = append(out, escape)
			case 'b':
				out = append(out, '\b')
			case 'f':
				out = append(out, '\f')
			case 'n':
				out = append(out, '\n')
			case 'r':
				out = append(out, '\r')
			case 't':
				out = append(out, '\t')
			case 'u':
				r, err := p.parseUnicodeEscape()
				if err != nil {
					return "", err
				}
				out = utf8.AppendRune(out, r)
			default:
				return "", p.errorf("invalid string escape \\%c", escape)
			}
			continue
		}
		if c < 0x20 {
			return "", p.errorf("invalid control character in string")
		}
		if c >= utf8.RuneSelf {
			r, size := utf8.DecodeRune(p.data[p.index:])
			if r == utf8.RuneError && size == 1 {
				out = utf8.AppendRune(out, utf8.RuneError)
				p.index++
				continue
			}
			out = append(out, p.data[p.index:p.index+size]...)
			p.index += size
			continue
		}
		out = append(out, c)
		p.index++
	}
	return "", p.errorf("unexpected end of string")
}

func (p *jitJSONParser) parseUnicodeEscape() (rune, error) {
	if p.index+4 > len(p.data) {
		return 0, p.errorf("short unicode escape")
	}
	value, err := strconv.ParseUint(string(p.data[p.index:p.index+4]), 16, 16)
	if err != nil {
		return 0, p.errorf("invalid unicode escape")
	}
	p.index += 4
	r := rune(value)
	if r < 0xd800 || r > 0xdfff {
		return r, nil
	}
	if r > 0xdbff || p.index+6 > len(p.data) || p.data[p.index] != '\\' || p.data[p.index+1] != 'u' {
		return utf8.RuneError, nil
	}
	low, err := strconv.ParseUint(string(p.data[p.index+2:p.index+6]), 16, 16)
	if err != nil || low < 0xdc00 || low > 0xdfff {
		return utf8.RuneError, nil
	}
	p.index += 6
	return utf16.DecodeRune(r, rune(low)), nil
}

func (p *jitJSONParser) parseNumber() (string, error) {
	start := p.index
	if err := p.consumeNumber(); err != nil {
		return "", err
	}
	return string(p.data[start:p.index]), nil
}

func (p *jitJSONParser) parseNumberView() (string, error) {
	start := p.index
	if err := p.consumeNumber(); err != nil {
		return "", err
	}
	raw := p.data[start:p.index]
	if len(raw) == 0 {
		return "", nil
	}
	return unsafe.String((*byte)(unsafe.SliceData(raw)), len(raw)), nil
}

func (p *jitJSONParser) consumeNumber() error {
	if p.consumeByte('-') && p.index >= len(p.data) {
		return p.errorf("invalid number")
	}
	if p.index >= len(p.data) {
		return p.errorf("invalid number")
	}
	if p.data[p.index] == '0' {
		p.index++
	} else {
		if p.data[p.index] < '1' || p.data[p.index] > '9' {
			return p.errorf("invalid number")
		}
		for p.index < len(p.data) && p.data[p.index] >= '0' && p.data[p.index] <= '9' {
			p.index++
		}
	}
	if p.consumeByte('.') {
		digits := p.index
		for p.index < len(p.data) && p.data[p.index] >= '0' && p.data[p.index] <= '9' {
			p.index++
		}
		if p.index == digits {
			return p.errorf("invalid number fraction")
		}
	}
	if p.index < len(p.data) && (p.data[p.index] == 'e' || p.data[p.index] == 'E') {
		p.index++
		if p.index < len(p.data) && (p.data[p.index] == '+' || p.data[p.index] == '-') {
			p.index++
		}
		digits := p.index
		for p.index < len(p.data) && p.data[p.index] >= '0' && p.data[p.index] <= '9' {
			p.index++
		}
		if p.index == digits {
			return p.errorf("invalid number exponent")
		}
	}
	return nil
}

func (p *jitJSONParser) skipSpace() {
	for p.index < len(p.data) && p.data[p.index] <= ' ' {
		switch p.data[p.index] {
		case ' ', '\t', '\r', '\n':
			p.index++
		default:
			return
		}
	}
}

func (p *jitJSONParser) consumeLiteral(literal string) bool {
	if p.index+len(literal) > len(p.data) || string(p.data[p.index:p.index+len(literal)]) != literal {
		return false
	}
	p.index += len(literal)
	return true
}

func (p *jitJSONParser) consumeByte(value byte) bool {
	if p.index < len(p.data) && p.data[p.index] == value {
		p.index++
		return true
	}
	return false
}

func (p *jitJSONParser) errorf(format string, args ...any) error {
	return fmt.Errorf("json: %s at byte %d", fmt.Sprintf(format, args...), p.index)
}
