// cfast-simd: fused NEON scan. One pass over the header block computes 64-bit
// bitmasks for '\n' and ':' (64 bytes per NEON iteration, simdjson-style
// movemask), then headers are parsed by walking bit positions. This replaces
// ~20 serially-dependent memchr() calls per message with ~5 independent chunk
// scans plus cheap bit iteration. No per-byte validation, zero-copy, same
// harness as cfast.c. `./cfast-simd check` diffs it against the scalar parser.
#include <arm_neon.h>
#include <pthread.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>
#include <unistd.h>

typedef struct {
    const char *name; size_t nlen;
    const char *val;  size_t vlen;
} hdr_t;

#define NLCAP 128   // newline positions tracked (max header lines + 1)
#define COCAP 256   // colon positions tracked (headers + stray body colons in last chunk)

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

// Fused mask walker: no position arrays. Walk the '\n' mask bit by bit; for each
// line the header colon is the first ':' bit below the newline bit, and colons
// consumed by a line (including ones inside the value) are cleared with one AND.
// The blank line shows up naturally as a line of length 1 ("\r").
static inline int parse_msg(const char *buf, size_t len, int is_req, hdr_t *h,
                            const char **body, size_t *body_len) {
    const char *end = buf + len;
    size_t line_start = 0, colon = 0;
    int have_colon = 0, first_line = 1, n = 0;
    for (size_t base = 0; base < len; base += 64) {
        uint64_t nl, co;
        if (base + 64 <= len) chunk_masks((const uint8_t *)buf + base, &nl, &co);
        else { uint8_t tmp[64] = {0}; memcpy(tmp, buf + base, len - base); chunk_masks(tmp, &nl, &co); }
        while (nl) {
            unsigned r = __builtin_ctzll(nl);
            size_t e = base + r;                       // '\n' position
            uint64_t low = (1ull << r) - 1;
            if (!have_colon && (co & low)) { colon = base + __builtin_ctzll(co & low); have_colon = 1; }
            co &= ~low;                                // drop colons consumed by this line
            nl &= nl - 1;
            if (first_line) {                          // request line / status line
                first_line = 0;
                const char *le = buf + e - 1;          // '\r'
                const char *sp1 = memchr(buf, ' ', le - buf);
                if (is_req) {
                    const char *sp2 = memchr(sp1 + 1, ' ', le - (sp1 + 1)); // path | version
                    (void)sp2;                                              // method/path/version framed
                } else {
                    int status = (sp1[1] - '0') * 100 + (sp1[2] - '0') * 10 + (sp1[3] - '0');
                    (void)status;
                }
                have_colon = 0;                        // discard any colon seen in the first line
                line_start = e + 1;
                continue;
            }
            if (e == line_start + 1) {                 // "\r\n" blank line: end of headers
                const char *p = buf + e + 1;
                *body = p; *body_len = end - p;
                return n;
            }
            const char *hle = buf + e - 1;             // '\r'
            const char *vs = buf + colon + 1;
            vs += (vs < hle && *vs == ' ');            // branchless: the usual single space
            while (vs < hle && (*vs == ' ' || *vs == '\t')) vs++;
            const char *ve = hle;
            while (ve > vs && (ve[-1] == ' ' || ve[-1] == '\t')) ve--;
            if (n < 64) { h[n].name = buf + line_start; h[n].nlen = colon - line_start; h[n].val = vs; h[n].vlen = ve - vs; }
            n++;
            have_colon = 0;
            line_start = e + 1;
        }
        // line spans into the next chunk: remember its colon before masks are refilled
        if (!have_colon && co) { colon = base + __builtin_ctzll(co); have_colon = 1; }
    }
    *body = end; *body_len = 0;                        // no blank line found
    return n;
}

static int parse_req(const char *buf, size_t len, hdr_t *h, const char **body, size_t *body_len) {
    return parse_msg(buf, len, 1, h, body, body_len);
}
static int parse_resp(const char *buf, size_t len, hdr_t *h, const char **body, size_t *body_len) {
    return parse_msg(buf, len, 0, h, body, body_len);
}

// ---------------- scalar reference (cfast.c parser) for `check` mode ----------------
static int parse_msg_scalar(const char *buf, size_t len, hdr_t *h, const char **body, size_t *body_len) {
    const char *end = buf + len;
    const char *nl = memchr(buf, '\n', len);
    const char *p = nl + 1;
    int n = 0;
    while (p < end) {
        if (*p == '\r') { p += 2; break; }
        const char *hnl = memchr(p, '\n', end - p);
        const char *hle = hnl - 1;
        const char *colon = memchr(p, ':', hle - p);
        const char *vs = colon + 1;
        while (vs < hle && (*vs == ' ' || *vs == '\t')) vs++;
        const char *ve = hle;
        while (ve > vs && (ve[-1] == ' ' || ve[-1] == '\t')) ve--;
        if (n < 64) { h[n].name = p; h[n].nlen = colon - p; h[n].val = vs; h[n].vlen = ve - vs; }
        n++;
        p = hnl + 1;
    }
    *body = p; *body_len = end - p;
    return n;
}

// ---------------- benchmark harness (same shape as cfast.c) ----------------
static const char *SIZES[] = {"256b", "1kb", "4kb", "10kb", "20kb", "64kb"};
static const int NSIZES = 6;
static const double RUN_SECONDS = 0.8;
#define RUNS 3

static unsigned char *slurp(const char *path, size_t *out_len) {
    FILE *f = fopen(path, "rb");
    if (!f) { perror(path); exit(1); }
    fseek(f, 0, SEEK_END); long n = ftell(f); fseek(f, 0, SEEK_SET);
    unsigned char *buf = malloc(n);
    if (fread(buf, 1, n, f) != (size_t)n) { perror("read"); exit(1); }
    fclose(f); *out_len = n; return buf;
}
static double now_s(void) {
    struct timespec ts; clock_gettime(CLOCK_MONOTONIC, &ts);
    return ts.tv_sec + ts.tv_nsec / 1e9;
}
typedef struct { const unsigned char *buf; size_t len; int is_req; double deadline; long count; volatile long sink; } warg;
static void *worker(void *p) {
    warg *a = p; long nn = 0, s = 0; const char *body; size_t bl; hdr_t h[64];
    do {
        for (int i = 0; i < 50000; i++) {
            s += a->is_req ? parse_req((const char *)a->buf, a->len, h, &body, &bl)
                           : parse_resp((const char *)a->buf, a->len, h, &body, &bl);
            __asm__ volatile("" : : "r"(h) : "memory"); // h "escapes": keep header stores alive
        }
        nn += 50000;
    } while (now_s() < a->deadline);
    a->count = nn; a->sink = s; return NULL;
}
static double bench(const unsigned char *buf, size_t len, int is_req, int threads, double *ns) {
    const char *body; size_t bl; volatile long s = 0; hdr_t h[64];
    for (int i = 0; i < 500000; i++) {
        s += is_req ? parse_req((const char *)buf, len, h, &body, &bl)
                    : parse_resp((const char *)buf, len, h, &body, &bl);
        __asm__ volatile("" : : "r"(h) : "memory");
    }
    pthread_t th[256]; warg args[256];
    double start = now_s(), deadline = start + RUN_SECONDS;
    for (int t = 0; t < threads; t++) { args[t] = (warg){buf, len, is_req, deadline, 0, 0}; pthread_create(&th[t], NULL, worker, &args[t]); }
    long total = 0;
    for (int t = 0; t < threads; t++) { pthread_join(th[t], NULL); total += args[t].count; }
    double el = now_s() - start; *ns = el * 1e9 / total; return total / el;
}
static void run_pass(unsigned char **req, size_t *reqL, unsigned char **resp, size_t *respL, int threads) {
    printf("  cfast-simd (C, NEON fused newline+colon bitmask scan, no validation)\n");
    printf("  %-6s %-5s | %9s %9s %9s | %9s\n", "body", "kind", "run1", "run2", "run3", "avg");
    printf("  ------------+-------------------------------+----------\n");
    for (int s = 0; s < NSIZES; s++)
        for (int rq = 1; rq >= 0; rq--) {
            const unsigned char *b = rq ? req[s] : resp[s]; size_t L = rq ? reqL[s] : respL[s];
            double r[RUNS], sum = 0, ns;
            for (int k = 0; k < RUNS; k++) { r[k] = bench(b, L, rq, threads, &ns) / 1e6; sum += r[k]; }
            printf("  %-6s %-5s | %9.2f %9.2f %9.2f | %9.2f\n", SIZES[s], rq ? "req" : "resp", r[0], r[1], r[2], sum / RUNS);
        }
}
static int check_one(const unsigned char *buf, size_t len, int is_req, const char *label) {
    hdr_t hs[64], hv[64]; const char *bs, *bv; size_t bls, blv;
    int ns = parse_msg_scalar((const char *)buf, len, hs, &bs, &bls);
    int nv = is_req ? parse_req((const char *)buf, len, hv, &bv, &blv)
                    : parse_resp((const char *)buf, len, hv, &bv, &blv);
    if (ns != nv || bs != bv || bls != blv) {
        printf("  FAIL %-10s n=%d/%d body=%p/%p len=%zu/%zu\n", label, ns, nv, (void *)bs, (void *)bv, bls, blv);
        return 1;
    }
    for (int i = 0; i < ns && i < 64; i++)
        if (hs[i].name != hv[i].name || hs[i].nlen != hv[i].nlen || hs[i].val != hv[i].val || hs[i].vlen != hv[i].vlen) {
            printf("  FAIL %-10s header %d: %.*s\n", label, i, (int)hs[i].nlen, hs[i].name);
            return 1;
        }
    printf("  ok   %-10s %2d headers, body %zu bytes\n", label, ns, bls);
    return 0;
}
int main(int argc, char **argv) {
    const char *dir = getenv("FIXTURES"); if (!dir) dir = "fixtures";
    int ncpu = (int)sysconf(_SC_NPROCESSORS_ONLN);
    unsigned char *req[NSIZES], *resp[NSIZES]; size_t reqL[NSIZES], respL[NSIZES];
    for (int s = 0; s < NSIZES; s++) {
        char path[512];
        snprintf(path, sizeof(path), "%s/req-%s.bin", dir, SIZES[s]); req[s] = slurp(path, &reqL[s]);
        snprintf(path, sizeof(path), "%s/resp-%s.bin", dir, SIZES[s]); resp[s] = slurp(path, &respL[s]);
    }
    if (argc > 1 && !strcmp(argv[1], "check")) {
        int bad = 0;
        char label[64];
        for (int s = 0; s < NSIZES; s++) {
            snprintf(label, sizeof(label), "req-%s", SIZES[s]);  bad += check_one(req[s], reqL[s], 1, label);
            snprintf(label, sizeof(label), "resp-%s", SIZES[s]); bad += check_one(resp[s], respL[s], 0, label);
        }
        return bad ? 1 : 0;
    }
    printf("\n========================= SINGLE CORE (1 thread) =========================\n");
    run_pass(req, reqL, resp, respL, 1);
    printf("\n========================= ALL CORES (%d threads) =========================\n", ncpu);
    run_pass(req, reqL, resp, respL, ncpu);
    return 0;
}
