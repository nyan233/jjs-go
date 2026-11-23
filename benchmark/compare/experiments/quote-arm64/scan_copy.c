// Standalone, bounded ASCII scan/copy experiment. Not linked into the Go codec.
// clang -O3 -mcpu=apple-m1 scan_copy.c -o /tmp/quote-scan
// clang -O3 -mcpu=apple-m1 -S scan_copy.c -o /tmp/quote-scan.s
#include <arm_neon.h>
#include <stdint.h>
#include <stdio.h>
#include <string.h>
#include <time.h>

static int special(unsigned char c) {
    return c < 32 || c >= 128 || c == '"' || c == '\\' || c == '<' || c == '>' || c == '&';
}
__attribute__((noinline)) size_t scan_copy_scalar(unsigned char *dst, const unsigned char *src, size_t n) {
    size_t i=0;
    for (;i<n;i++) {if(special(src[i]))break;dst[i]=src[i];}
    return i;
}
__attribute__((noinline)) size_t scan_copy_compare(unsigned char *dst, const unsigned char *src, size_t n) {
    size_t i=0;
    for (;n-i>=16;i+=16) {
        uint8x16_t x=vld1q_u8(src+i);
        uint8x16_t m=vorrq_u8(vcltq_u8(x,vdupq_n_u8(32)), vcgeq_u8(x,vdupq_n_u8(128)));
        m=vorrq_u8(m,vceqq_u8(x,vdupq_n_u8('"')));
        m=vorrq_u8(m,vceqq_u8(x,vdupq_n_u8('\\')));
        m=vorrq_u8(m,vceqq_u8(x,vdupq_n_u8('<')));
        m=vorrq_u8(m,vceqq_u8(x,vdupq_n_u8('>')));
        m=vorrq_u8(m,vceqq_u8(x,vdupq_n_u8('&')));
        if(vmaxvq_u8(m))break;
        vst1q_u8(dst+i,x);
    }
    return i+scan_copy_scalar(dst+i,src+i,n-i);
}
__attribute__((noinline)) size_t scan_copy_table(unsigned char *dst, const unsigned char *src, size_t n) {
    // Intersect high/low-nibble class bitsets. All high bytes are special.
    const uint8_t lo[16]={17,17,19,17,17,17,19,17,17,17,17,17,29,17,21,17};
    const uint8_t hi[16]={1,1,2,4,0,8,0,0,16,16,16,16,16,16,16,16};
    uint8x16_t l=vld1q_u8(lo), h=vld1q_u8(hi);
    size_t i=0;
    for (;n-i>=16;i+=16) {
        uint8x16_t x=vld1q_u8(src+i);
        uint8x16_t a=vqtbl1q_u8(l,vandq_u8(x,vdupq_n_u8(15)));
        uint8x16_t b=vqtbl1q_u8(h,vshrq_n_u8(x,4));
        if(vmaxvq_u8(vandq_u8(a,b)))break;
        vst1q_u8(dst+i,x);
    }
    return i+scan_copy_scalar(dst+i,src+i,n-i);
}
static double now(void) {
    struct timespec t;clock_gettime(CLOCK_MONOTONIC,&t);return (double)t.tv_sec+t.tv_nsec*1e-9;
}
int main(void) {
    unsigned char src[4096],dst[4096];memset(src,'a',sizeof(src));
    size_t (*functions[])(unsigned char*,const unsigned char*,size_t)={scan_copy_scalar,scan_copy_compare,scan_copy_table};
    const char *names[]={"scalar","compare","table"};
    const size_t sizes[]={8,32,64,256,1024,4096};
    volatile size_t checksum=0;
    for(size_t k=0;k<sizeof(sizes)/sizeof(sizes[0]);k++) {
        size_t n=sizes[k],loops=100000000/n;
        for(int j=0;j<3;j++) {
            double start=now();
            for(size_t i=0;i<loops;i++)checksum+=functions[j](dst,src,n);
            printf("%s %zu %.3f ns/call\n",names[j],n,(now()-start)*1e9/loops);
        }
    }
    return checksum==0;
}
