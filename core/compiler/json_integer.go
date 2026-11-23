package compiler

// parseInteger validates JSON integer syntax and accumulates its value in one
// pass. Signed results use two's complement so MinInt64 remains representable.
func (p *jitJSONParser) parseInteger(bits int, signed bool) (uint64, error) {
	negative := p.consumeByte('-')
	if negative && !signed {
		return 0, p.errorf("negative number for unsigned integer")
	}
	if p.index >= len(p.data) || p.data[p.index] < '0' || p.data[p.index] > '9' {
		return 0, p.errorf("invalid integer")
	}
	limit := ^uint64(0) >> (64 - bits)
	if signed {
		limit >>= 1
		if negative {
			limit++
		}
	}
	cutoff, lastDigit := limit/10, limit%10
	start := p.index
	var value uint64
	for p.index < len(p.data) {
		digit := p.data[p.index] - '0'
		if digit > 9 {
			break
		}
		if p.index > start && p.data[start] == '0' {
			return 0, p.errorf("leading zero in integer")
		}
		if value > cutoff || value == cutoff && uint64(digit) > lastDigit {
			return 0, p.errorf("integer overflow")
		}
		value = value*10 + uint64(digit)
		p.index++
	}
	if p.index < len(p.data) {
		switch p.data[p.index] {
		case '.', 'e', 'E':
			return 0, p.errorf("non-integer number")
		}
	}
	if negative {
		value = -value
	}
	return value, nil
}
