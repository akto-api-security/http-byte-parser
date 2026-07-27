# CLAUDE.md — agent guide

## What this is
Parse a raw HTTP request/response `[]byte` (one complete message per buffer) into
fields, then into the Akto Kafka JSON payload — **zero-copy, zero-allocation**.
Motivation: the production pipeline used `http.ReadRequest` + header maps +
`json.Marshal`, was allocation/GC-bound, and saturated CPU at ~30k pairs/sec. This
repo replaces that with slice-in-place parsing. **The shipped code is Go**; the C
parsers are speed references, not shipped.

## Architecture
```
go-approach/           Go module `goapproach` (SHIPPED)
  httpparser/          parser: ParseRequest/ParseResponse -> *Request/*Response of []byte slices; safe (errors, never panics)
  kafkashape/          encoder: BuildFull(reqBuf,respBuf,*Meta) -> Kafka JSON (0-alloc, one pass); ProcessPairs/ProcessPairsFunc = batch loop
  cparse/              cgo bridge: exposes the C parsers to Go so the SAME Go tests drive them (arm64+cgo only)
  cmd/bench            Go parser throughput binary
  cmd/shapebench       encoder benchmark
  cmd/orchestrate      paced load driver -> cpu.prof
cfast/                 C parsers + shared harness
  common.h  common.c   the shared C contract + the ONE benchmark harness (fixture load, timing, anti-DCE, dump, main)
  cfast.c              scalar memchr parser  (implements parse())
  cfast-simd.c         NEON fused-bitmask parser (arm64 only; implements parse())
  pico-adapter.c       makes picohttpparser conform to parse()
pico-neon/             vendored picohttpparser + an ARM NEON port
fixtures/              req-/resp-<size>.bin = COMPLETE raw HTTP messages (headers+body), built by gen-fixtures.py from json-files/*.json (bodies)
```

## The C contract (how to add/iterate a C variant)
Every C parser implements ONE function; the shared `common.c` harness calls it:
```c
// common.h: fields are zero-copy slices into buf
typedef struct { const char *p; int len; } slice_t;
typedef struct { slice_t method,path,version; int status_code; slice_t reason;
                 slice_t hname[MAXH],hval[MAXH]; int nheaders; slice_t body; } http_msg;
int parse(const char *buf, size_t len, int is_req, http_msg *out);  // fill out, return header count
```
To add a variant: write one `parse()` in a new `.c`, then `cc -O3 common.c yourvariant.c -o bin/x`.
Never put timing/harness code in a variant — that's what let a dead-code-elimination bug
inflate a number before (the harness lives in `common.c` exactly once).

## Invariants — must hold; tests enforce them
1. **0 allocations** on the Go hot path (`ParseRequest/Response`, `BuildFull`). Verify `-benchmem` shows `0 allocs/op`.
2. **Safe**: the Go parser returns `ErrMalformed`, never panics. (C parsers are intentionally UNSAFE — no validation, crash on malformed — which is why they don't ship.)
3. **Zero-copy**: fields are slices into the input buffer; don't copy, don't retain past the buffer's life, don't mutate buf while slices are live.
4. **Valid JSON always**, incl. invalid-UTF-8 bodies (replaced with U+FFFD). `kafkashape` only.
5. **Output keys**: `BuildFull` emits every legacy key; headers = nested objects; other values = strings; duplicate headers preserved.
6. **Scope**: one complete message per buffer. No streaming/chunked/pipelined framing.
7. **Reuse `Parser`/`Builder` per goroutine; never share** (mutable scratch).

## Correctness model (one harness, both languages)
- Oracle = Go `net/http`. `httpparser` is checked by differential-vs-`net/http`, 2000-case
  generative, no-panic fuzz, golden, and an edge table (`go-approach/httpparser`, `kafkashape`).
- **The C parsers run through the SAME Go tests via cgo** (`go-approach/cparse`): the differential
  and generative suites drive `cfast`/`cfast-simd` too. That's why there's no separate C test harness.
- Safety tests (fuzz/malformed) are Go-only by design — unsafe C would crash, not fail gracefully.
- `check`/`dump`: `./bin/cfast dump` prints parsed fields to eyeball.

## Iterating — commands
```bash
cd go-approach
go test ./...                                     # correctness gate — MUST stay green (arm64+cgo drives C too)
go test ./httpparser/ -bench=. -benchmem -run=^$  # Go parser numbers
./build.sh && FIXTURES=fixtures ./bin/cfast-simd  # rebuild + run a C variant
```
Benchmark C variants natively (`./bin/*`), **never through cgo** — the boundary distorts a ~20 ns parse.

## Current numbers (Apple M-series arm64, 1 KB, single core; all C variants do equal work via common.c)
| parser | M/s | notes |
|---|---|---|
| net/http | ~1 | 16 allocs, GC-bound (the thing replaced) |
| Go httpparser (shipped) | ~14–16 | 0 alloc, safe |
| pico (C) | ~12 | validates; SIMD is x86-only so scalar on arm |
| cfast scalar (C) | ~25 | memchr, no validation |
| cfast-simd (C) | ~38 | NEON fused mask; ~1.5× over scalar at equal work |

Notes: numbers move with load — `allocs/op` is the stable signal. At 30k pairs/sec the shipped
pipeline is ~2% of a core, so the real win was **0 allocations**, not raw ns. `cfast-simd` is
**arm64-only** (NEON); the Docker/k8s image ships only Go `bench` + scalar `cfast` (amd64-portable).
An earlier "~50 M/s cfast" was a DCE artifact — fixed by moving to the shared `common.c` harness.

## Gotchas
- `cparse` is gated `//go:build cgo && arm64` so `go test ./...` stays green on amd64/CGO-off.
- Editing Fable's `cfast-simd.c`: the NEON scan logic is deliberate; keep it, only touch the struct-fill.
- Fixtures are generated; run `gen-fixtures.py` if `fixtures/` is missing.
