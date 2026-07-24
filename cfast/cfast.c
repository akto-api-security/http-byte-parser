// cfast — scalar memchr parser. Implements the common parse() contract.
// Zero-copy (all fields are slices into buf), NO validation (assumes well-formed
// input; unsafe on malformed data — see the Go parser for the safe version).
#include "common.h"
#include <string.h>

int parse(const char *buf, size_t len, int is_req, http_msg *out) {
    const char *end = buf + len;
    const char *nl = memchr(buf, '\n', len);
    if (!nl) { out->nheaders = 0; out->body = (slice_t){end, 0}; return 0; }
    const char *le = nl - 1; // '\r'

    const char *sp1 = memchr(buf, ' ', le - buf);
    if (is_req) {
        out->method = (slice_t){buf, (int)(sp1 - buf)};
        const char *sp2 = memchr(sp1 + 1, ' ', le - (sp1 + 1));
        out->path = (slice_t){sp1 + 1, (int)(sp2 - (sp1 + 1))};
        out->version = (slice_t){sp2 + 1, (int)(le - (sp2 + 1))};
    } else {
        out->version = (slice_t){buf, (int)(sp1 - buf)};
        out->status_code = (sp1[1] - '0') * 100 + (sp1[2] - '0') * 10 + (sp1[3] - '0');
        const char *sp2 = memchr(sp1 + 1, ' ', le - (sp1 + 1)); // space after the code
        if (sp2 && sp2 < le) out->reason = (slice_t){sp2 + 1, (int)(le - (sp2 + 1))};
        else out->reason = (slice_t){le, 0};
    }

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
        if (n < MAXH) {
            out->hname[n] = (slice_t){p, (int)(colon - p)};
            out->hval[n]  = (slice_t){vs, (int)(ve - vs)};
        }
        n++;
        p = hnl + 1;
    }
    out->nheaders = n;
    out->body = (slice_t){p, (int)(end - p)};
    return n;
}
