// common.h — the shared contract every C parser variant conforms to.
// A parser fills http_msg (all fields are zero-copy slices into the input buf)
// and returns the header count (or a negative error, for validating parsers).
#ifndef CPARSE_COMMON_H
#define CPARSE_COMMON_H
#include <stddef.h>

#define MAXH 128

typedef struct { const char *p; int len; } slice_t; // window into the input buffer

typedef struct {
    slice_t method, path, version;   // request line
    int     status_code;             // response
    slice_t reason;                  // response reason phrase
    slice_t hname[MAXH], hval[MAXH]; // headers
    int     nheaders;
    slice_t body;
} http_msg;

// Every variant implements exactly this. `is_req` selects request vs response.
int parse(const char *buf, size_t len, int is_req, http_msg *out);

#endif
