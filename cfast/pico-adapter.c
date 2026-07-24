// pico-adapter — makes picohttpparser conform to the common parse() contract.
// It does NOT parse anything itself; it calls phr_parse_request/response and
// translates the library's output (struct phr_header + separate method/path
// out-params) into our http_msg. picohttpparser validates input, so on malformed
// data it returns a negative code (which we propagate) instead of crashing.
#include "common.h"
#include "picohttpparser.h"

int parse(const char *buf, size_t len, int is_req, http_msg *out) {
    struct phr_header h[MAXH];
    size_t nh = MAXH;
    int consumed; // bytes up to end of headers (pico's return value on success)

    if (is_req) {
        const char *method, *path; size_t ml, pl; int minor;
        consumed = phr_parse_request(buf, len, &method, &ml, &path, &pl, &minor, h, &nh, 0);
        if (consumed < 0) { out->nheaders = 0; return consumed; }
        out->method  = (slice_t){method, (int)ml};
        out->path    = (slice_t){path, (int)pl};
        out->version = (slice_t){"HTTP/1.x", 8}; // pico gives only the minor version
    } else {
        const char *msg; size_t msgl; int minor, status;
        consumed = phr_parse_response(buf, len, &minor, &status, &msg, &msgl, h, &nh, 0);
        if (consumed < 0) { out->nheaders = 0; return consumed; }
        out->status_code = status;
        out->reason = (slice_t){msg, (int)msgl};
    }

    for (size_t i = 0; i < nh; i++) {
        out->hname[i] = (slice_t){h[i].name, (int)h[i].name_len};
        out->hval[i]  = (slice_t){h[i].value, (int)h[i].value_len};
    }
    out->nheaders = (int)nh;
    out->body = (slice_t){buf + consumed, (int)(len - (size_t)consumed)};
    return (int)nh;
}
