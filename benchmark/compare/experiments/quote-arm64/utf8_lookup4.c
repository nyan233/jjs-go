// Standalone 16-byte lookup4 quoting-prefix experiment; not linked into Go.
// Tables adapted from simdutf; https://arxiv.org/abs/2010.03090
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

#include <arm_neon.h>
#include <stddef.h>
#include <stdint.h>
#include <string.h>

// The caller supplies at least 16 readable and 16 writable bytes. Return zero
// on an escape/error; otherwise copy only a prefix of complete UTF-8 runes.
__attribute__((noinline)) size_t quote_utf8_prefix(uint8_t *dst, const uint8_t *src) {
    static const uint8_t prev_hi[16]={2,2,2,2,2,2,2,2,128,128,128,128,33,1,21,73};
    static const uint8_t prev_lo[16]={231,163,131,131,139,203,203,203,203,203,203,203,203,219,203,203};
    static const uint8_t curr_hi[16]={1,1,1,1,1,1,1,1,230,174,186,186,1,1,1,1};
    static const uint8_t json_lo[16]={1,1,3,1,1,1,3,1,1,1,1,1,13,1,5,1};
    static const uint8_t json_hi[16]={1,1,2,4,0,8,0,0,0,0,0,0,0,0,0,0};
    uint8x16_t x=vld1q_u8(src), zero=vdupq_n_u8(0), mask=vdupq_n_u8(15);
    uint8x16_t p1=vextq_u8(zero,x,15), p2=vextq_u8(zero,x,14), p3=vextq_u8(zero,x,13);
    uint8x16_t e=vandq_u8(vqtbl1q_u8(vld1q_u8(prev_hi),vshrq_n_u8(p1,4)),
                        vqtbl1q_u8(vld1q_u8(prev_lo),vandq_u8(p1,mask)));
    e=vandq_u8(e,vqtbl1q_u8(vld1q_u8(curr_hi),vshrq_n_u8(x,4)));
    uint8x16_t more=vorrq_u8(vcgeq_u8(p2,vdupq_n_u8(0xe0)),vcgeq_u8(p3,vdupq_n_u8(0xf0)));
    e=veorq_u8(e,vandq_u8(more,vdupq_n_u8(0x80)));
    uint8x16_t sep=vandq_u8(vceqq_u8(p2,vdupq_n_u8(0xe2)),vceqq_u8(p1,vdupq_n_u8(0x80)));
    sep=vandq_u8(sep,vceqq_u8(vandq_u8(x,vdupq_n_u8(0xfe)),vdupq_n_u8(0xa8)));
    uint8x16_t json=vandq_u8(vqtbl1q_u8(vld1q_u8(json_lo),vandq_u8(x,mask)),
                           vqtbl1q_u8(vld1q_u8(json_hi),vshrq_n_u8(x,4)));
    if(vmaxvq_u8(vorrq_u8(e,vorrq_u8(sep,json)))) return 0;
    size_t n=16;
    if(src[15]>=0xc0) n=15;
    else if(src[14]>=0xe0) n=14;
    else if(src[13]>=0xf0) n=13;
    // All accepted prefixes have length 13..16, so overlapping 8-byte stores
    // copy exactly n bytes, with no speculative write beyond the return value.
    uint64_t a,b;
    memcpy(&a,src,8);memcpy(&b,src+n-8,8);
    memcpy(dst,&a,8);memcpy(dst+n-8,&b,8);
    return n;
}
