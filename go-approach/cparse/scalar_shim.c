#include "shim.h"
#define parse cf_scalar_parse
#include "cfast.c"
#undef parse

int cf_scalar_req(const char *buf, size_t len, int *no, int *nl, int *vo, int *vl,
                  int maxh, int *bo, int *bl) {
    http_msg m; cf_scalar_parse(buf, len, 1, &m);
    return cparse_fill(&m, buf, no, nl, vo, vl, maxh, bo, bl);
}
int cf_scalar_resp(const char *buf, size_t len, int *no, int *nl, int *vo, int *vl,
                   int maxh, int *bo, int *bl) {
    http_msg m; cf_scalar_parse(buf, len, 0, &m);
    return cparse_fill(&m, buf, no, nl, vo, vl, maxh, bo, bl);
}
