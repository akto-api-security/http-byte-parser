#include "shim.h"
#include "cshape.h"
#define parse cf_enc_parse
#include "cfast.c"
#undef parse
#include "cshape.c"

#define SL(s) {(s), (int)sizeof(s) - 1}
static const cmeta SAMPLE = {
    .source_ip = SL("10.0.0.1"), .dest_ip = SL("10.0.0.2"),
    .akto_account_id = SL("1000000"), .source = SL("MIRRORING"),
    .daemonset_id = SL("ds-1"), .process_name = SL("svc"), .tag = SL("{\"env\":\"prod\"}"),
    .time_unix = 1690000000, .vxlan_id = 42, .direction = 1,
    .is_pending = 0, .enable_graph = 1, .process_id = 1234, .socket_id = 7,
};
static obuf g_ob;

// parse both buffers with the C parser, cshape-encode with SAMPLE meta; returns
// len and sets *outp to the (C-owned, reused) buffer. Go copies via C.GoBytes.
int cshape_encode_pair(const char *reqBuf, size_t rl, const char *respBuf, size_t sl, char **outp) {
    http_msg req, resp;
    cf_enc_parse(reqBuf, rl, 1, &req);
    cf_enc_parse(respBuf, sl, 0, &resp);
    cshape_encode(&req, &resp, &SAMPLE, &g_ob);
    *outp = g_ob.p;
    return (int)g_ob.len;
}
