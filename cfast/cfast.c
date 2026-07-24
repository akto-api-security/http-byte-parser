// cfast: the fastest-C-on-Mac approach — a zero-copy HTTP parser that finds
// delimiters with memchr() (NEON-accelerated single-byte search in libc, the C
// equivalent of Go's bytes.IndexByte) and does NO per-byte validation. This is
// the C twin of our Go V3 parser. Built-in benchmark: single-core + all-cores,
// requests + responses, 3 runs, M/s.
#include <pthread.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>
#include <unistd.h>

typedef struct {
    const char *name; size_t nlen;
    const char *val;  size_t vlen;
} hdr_t;

// Returns number of headers; sets *body/*body_len. Zero-copy (pointers alias buf).
// h is caller-supplied (room for 64) so the compiler cannot dead-store-eliminate
// the header extraction work.
static int parse_req(const char *buf, size_t len, hdr_t *h, const char **body, size_t *body_len) {
    const char *end = buf + len;
    const char *nl = memchr(buf, '\n', len);            // end of request line
    const char *le = nl - 1;                            // '\r'
    const char *sp1 = memchr(buf, ' ', le - buf);       // method | path
    const char *sp2 = memchr(sp1 + 1, ' ', le - (sp1 + 1)); // path | version
    (void)sp2;                                          // method/path/version framed
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

static int parse_resp(const char *buf, size_t len, hdr_t *h, const char **body, size_t *body_len) {
    const char *end = buf + len;
    const char *nl = memchr(buf, '\n', len);
    const char *le = nl - 1;
    const char *sp1 = memchr(buf, ' ', le - buf);       // version | code
    int status = (sp1[1] - '0') * 100 + (sp1[2] - '0') * 10 + (sp1[3] - '0');
    (void)status;
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

// ---------------- benchmark harness (same shape as bench_pico) ----------------
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
    printf("  cfast (C, memchr/NEON single-byte scan, no validation)\n");
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
int main(void) {
    const char *dir = getenv("FIXTURES"); if (!dir) dir = "fixtures";
    int ncpu = (int)sysconf(_SC_NPROCESSORS_ONLN);
    unsigned char *req[NSIZES], *resp[NSIZES]; size_t reqL[NSIZES], respL[NSIZES];
    for (int s = 0; s < NSIZES; s++) {
        char path[512];
        snprintf(path, sizeof(path), "%s/req-%s.bin", dir, SIZES[s]); req[s] = slurp(path, &reqL[s]);
        snprintf(path, sizeof(path), "%s/resp-%s.bin", dir, SIZES[s]); resp[s] = slurp(path, &respL[s]);
    }
    printf("\n========================= SINGLE CORE (1 thread) =========================\n");
    run_pass(req, reqL, resp, respL, 1);
    printf("\n========================= ALL CORES (%d threads) =========================\n", ncpu);
    run_pass(req, reqL, resp, respL, ncpu);
    return 0;
}
