// Lookup4 UTF-8 error tables adapted from simdutf.
// https://github.com/simdutf/simdutf/blob/master/src/generic/utf8_validation/utf8_lookup4_algorithm.h
// Algorithm: https://arxiv.org/abs/2010.03090
//
// Copyright 2021 The simdutf authors
//
// Permission is hereby granted, free of charge, to any person obtaining a copy of
// this software and associated documentation files (the "Software"), to deal in
// the Software without restriction, including without limitation the rights to
// use, copy, modify, merge, publish, distribute, sublicense, and/or sell copies of
// the Software, and to permit persons to whom the Software is furnished to do so,
// subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in all
// copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY, FITNESS
// FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR
// COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER
// IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN
// CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.

// Three 16-entry tables indexed by previous high nibble, previous low nibble
// and current high nibble. Their intersection identifies adjacent-byte errors:
// bits 0/1: missing/unexpected continuation; bit 2: overlong three-byte form;
// bit 3: above U+10FFFF; bit 4: surrogate; bit 5: overlong two-byte form;
// bit 6: overlong four-byte form or F5..FF followed by 80..8F;
// bit 7: consecutive continuations (XOR with required third/fourth positions).
DATA ·jsonQuoteUTF8+0(SB)/8, $0x0202020202020202
DATA ·jsonQuoteUTF8+8(SB)/8, $0x4915012180808080
DATA ·jsonQuoteUTF8+16(SB)/8, $0xcbcbcb8b8383a3e7
DATA ·jsonQuoteUTF8+24(SB)/8, $0xcbcbdbcbcbcbcbcb
DATA ·jsonQuoteUTF8+32(SB)/8, $0x0101010101010101
DATA ·jsonQuoteUTF8+40(SB)/8, $0x01010101babaaee6
GLOBL ·jsonQuoteUTF8(SB), RODATA|NOPTR, $48
