// common.c — the ONE shared benchmark/harness for every C parser variant.
// Build:  cc -O3 common.c <variant>.c -o <variant>
// The variant supplies parse(); this file supplies everything else, so all
// variants load fixtures, time, and defeat dead-code-elimination identically.
//
//   ./<variant>            single-core + all-cores throughput table (M/s)
//   ./<variant> dump       parse req/resp-1kb and print the fields (eyeball check)
#include "common.h"
#include <pthread.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>
#include <unistd.h>

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

// one unit of work: full parse into a reused msg, kept alive so nothing is DCE'd.
static inline long do_parse(const unsigned char *buf, size_t len, int is_req, http_msg *m) {
    int n = parse((const char *)buf, len, is_req, m);
    __asm__ volatile("" : : "r"(m) : "memory"); // the filled struct "escapes"
    return n + m->body.len;
}

typedef struct { const unsigned char *buf; size_t len; int is_req; double deadline; long count; volatile long sink; } warg;
static void *worker(void *p) {
    warg *a = p; long nn = 0, s = 0; http_msg m;
    do {
        for (int i = 0; i < 50000; i++) s += do_parse(a->buf, a->len, a->is_req, &m);
        nn += 50000;
    } while (now_s() < a->deadline);
    a->count = nn; a->sink = s; return NULL;
}
static double bench(const unsigned char *buf, size_t len, int is_req, int threads, double *ns) {
    http_msg m; volatile long s = 0;
    for (int i = 0; i < 500000; i++) s += do_parse(buf, len, is_req, &m); // warmup
    pthread_t th[256]; warg args[256];
    double start = now_s(), deadline = start + RUN_SECONDS;
    for (int t = 0; t < threads; t++) { args[t] = (warg){buf, len, is_req, deadline, 0, 0}; pthread_create(&th[t], NULL, worker, &args[t]); }
    long total = 0;
    for (int t = 0; t < threads; t++) { pthread_join(th[t], NULL); total += args[t].count; }
    double el = now_s() - start; *ns = el * 1e9 / total; return total / el;
}
static void run_pass(unsigned char **req, size_t *reqL, unsigned char **resp, size_t *respL, int threads) {
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

static void dump(const char *dir) {
    char path[512]; size_t rl, sl;
    snprintf(path, sizeof(path), "%s/req-1kb.bin", dir); unsigned char *rq = slurp(path, &rl);
    snprintf(path, sizeof(path), "%s/resp-1kb.bin", dir); unsigned char *rs = slurp(path, &sl);
    http_msg m;
    parse((const char *)rq, rl, 1, &m);
    printf("REQUEST  method=%.*s path=%.*s version=%.*s headers=%d body=%d\n",
           m.method.len, m.method.p, m.path.len, m.path.p, m.version.len, m.version.p, m.nheaders, m.body.len);
    for (int i = 0; i < m.nheaders; i++)
        printf("   %.*s: %.*s\n", m.hname[i].len, m.hname[i].p, m.hval[i].len, m.hval[i].p);
    parse((const char *)rs, sl, 0, &m);
    printf("RESPONSE version=%.*s status=%d reason=%.*s headers=%d body=%d\n",
           m.version.len, m.version.p, m.status_code, m.reason.len, m.reason.p, m.nheaders, m.body.len);
}

int main(int argc, char **argv) {
    const char *dir = getenv("FIXTURES"); if (!dir) dir = "fixtures";
    if (argc > 1 && strcmp(argv[1], "dump") == 0) { dump(dir); return 0; }
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
