#ifndef CPARSE_SHIM_H
#define CPARSE_SHIM_H
#include <stddef.h>
#include "common.h"

// Go-facing wrappers (defined in scalar_shim.c / simd_shim.c).
int cf_scalar_req(const char*, size_t, int*, int*, int*, int*, int, int*, int*);
int cf_scalar_resp(const char*, size_t, int*, int*, int*, int*, int, int*, int*);
int cf_simd_req(const char*, size_t, int*, int*, int*, int*, int, int*, int*);
int cf_simd_resp(const char*, size_t, int*, int*, int*, int*, int, int*, int*);

// cshape encoder (defined in cshape_shim.c): parse both + encode, returns len,
// sets *outp to the C-owned reused buffer (Go copies it).
int cshape_encode_pair(const char*, size_t, const char*, size_t, char**);

// shared: map a parsed http_msg into flat offset arrays for Go.
static inline int cparse_fill(http_msg *m, const char *buf, int *no, int *nl,
                              int *vo, int *vl, int maxh, int *bodyOff, int *bodyLen) {
    int mm = m->nheaders < maxh ? m->nheaders : maxh;
    for (int i = 0; i < mm; i++) {
        no[i] = (int)(m->hname[i].p - buf); nl[i] = m->hname[i].len;
        vo[i] = (int)(m->hval[i].p - buf);  vl[i] = m->hval[i].len;
    }
    *bodyOff = (int)(m->body.p - buf); *bodyLen = m->body.len;
    return m->nheaders;
}
#endif
