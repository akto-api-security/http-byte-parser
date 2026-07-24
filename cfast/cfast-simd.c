// cfast-simd — fused NEON scan (implements the common parse() contract).
// One pass computes 64-bit '\n' and ':' bitmasks per 64-byte chunk (simdjson-
// style movemask), then walks bit positions: the header colon is the first ':'
// below the newline bit, consumed colons are cleared with one AND, and the blank
// line falls out as a length-1 line. Zero-copy, NO validation. Original design
// by the optimizer pass; only the harness was factored out into common.c and the
// output widened to fill the full http_msg (method/path/version/status too).
#include "common.h"
#include <arm_neon.h>
#include <stdint.h>
#include <string.h>

// 64 bytes of compare results (4x16 0xFF/0x00 lanes) -> 64-bit mask, 1 bit/byte.
static inline uint64_t mask64(uint8x16_t e0, uint8x16_t e1, uint8x16_t e2, uint8x16_t e3) {
    const uint8x16_t bits = {1,2,4,8,16,32,64,128, 1,2,4,8,16,32,64,128};
    uint8x16_t s0 = vpaddq_u8(vandq_u8(e0, bits), vandq_u8(e1, bits));
    uint8x16_t s1 = vpaddq_u8(vandq_u8(e2, bits), vandq_u8(e3, bits));
    s0 = vpaddq_u8(s0, s1);
    s0 = vpaddq_u8(s0, s0);
    return vgetq_lane_u64(vreinterpretq_u64_u8(s0), 0);
}
static inline void chunk_masks(const uint8_t *p, uint64_t *nl, uint64_t *co) {
    uint8x16_t c0 = vld1q_u8(p), c1 = vld1q_u8(p + 16), c2 = vld1q_u8(p + 32), c3 = vld1q_u8(p + 48);
    uint8x16_t NL = vdupq_n_u8('\n'), CO = vdupq_n_u8(':');
    *nl = mask64(vceqq_u8(c0, NL), vceqq_u8(c1, NL), vceqq_u8(c2, NL), vceqq_u8(c3, NL));
    *co = mask64(vceqq_u8(c0, CO), vceqq_u8(c1, CO), vceqq_u8(c2, CO), vceqq_u8(c3, CO));
}

int parse(const char *buf, size_t len, int is_req, http_msg *out) {
    const char *end = buf + len;
    size_t line_start = 0, colon = 0;
    int have_colon = 0, first_line = 1, n = 0;

    for (size_t base = 0; base < len; base += 64) {
        uint64_t nl, co;
        if (base + 64 <= len) chunk_masks((const uint8_t *)buf + base, &nl, &co);
        else { uint8_t tmp[64] = {0}; memcpy(tmp, buf + base, len - base); chunk_masks(tmp, &nl, &co); }
        while (nl) {
            unsigned r = __builtin_ctzll(nl);
            size_t e = base + r;               // '\n' position
            uint64_t low = (1ull << r) - 1;
            if (!have_colon && (co & low)) { colon = base + __builtin_ctzll(co & low); have_colon = 1; }
            co &= ~low;                         // drop colons consumed by this line
            nl &= nl - 1;

            if (first_line) {
                first_line = 0;
                const char *le = buf + e - 1;   // '\r'
                const char *sp1 = memchr(buf, ' ', le - buf);
                if (is_req) {
                    out->method = (slice_t){buf, (int)(sp1 - buf)};
                    const char *sp2 = memchr(sp1 + 1, ' ', le - (sp1 + 1));
                    out->path = (slice_t){sp1 + 1, (int)(sp2 - (sp1 + 1))};
                    out->version = (slice_t){sp2 + 1, (int)(le - (sp2 + 1))};
                } else {
                    out->version = (slice_t){buf, (int)(sp1 - buf)};
                    out->status_code = (sp1[1] - '0') * 100 + (sp1[2] - '0') * 10 + (sp1[3] - '0');
                    const char *sp2 = memchr(sp1 + 1, ' ', le - (sp1 + 1));
                    if (sp2 && sp2 < le) out->reason = (slice_t){sp2 + 1, (int)(le - (sp2 + 1))};
                    else out->reason = (slice_t){le, 0};
                }
                have_colon = 0;
                line_start = e + 1;
                continue;
            }
            if (e == line_start + 1) {          // "\r\n" blank line: end of headers
                const char *p = buf + e + 1;
                out->nheaders = n;
                out->body = (slice_t){p, (int)(end - p)};
                return n;
            }
            const char *hle = buf + e - 1;      // '\r'
            const char *vs = buf + colon + 1;
            vs += (vs < hle && *vs == ' ');     // branchless: the usual single space
            while (vs < hle && (*vs == ' ' || *vs == '\t')) vs++;
            const char *ve = hle;
            while (ve > vs && (ve[-1] == ' ' || ve[-1] == '\t')) ve--;
            if (n < MAXH) {
                out->hname[n] = (slice_t){buf + line_start, (int)(colon - line_start)};
                out->hval[n]  = (slice_t){vs, (int)(ve - vs)};
            }
            n++;
            have_colon = 0;
            line_start = e + 1;
        }
        if (!have_colon && co) { colon = base + __builtin_ctzll(co); have_colon = 1; }
    }
    out->nheaders = n;
    out->body = (slice_t){end, 0};              // no blank line found
    return n;
}
