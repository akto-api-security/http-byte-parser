# go-approach — zero-copy HTTP parser + Kafka-shape encoder

## Your task

**Make this faster** — higher throughput and/or fewer allocations — **without breaking
correctness or the invariants below.** Every change must keep `go test ./...` green and
the hot path at **0 allocations**. Beat the baseline numbers; prove it with `go test -bench`.

## What it does

Parses one raw HTTP request + one raw HTTP response (each a complete message in a
`[]byte`) and emits the Kafka JSON payload. Replaces a `net/http` pipeline that was
allocation/GC-bound.

```
httpparser/       zero-copy parser: ParseRequest/ParseResponse -> fields as []byte slices into the buffer
kafkashape/       the encoder + orchestrator:
                    BuildFull(reqBuf, respBuf, *Meta) -> Kafka JSON, one pass, hand-rolled  (per-pair, hot path)
                    ProcessPairs / ProcessPairsFunc([]Pair, *Meta)                          (batch: loop + log-and-skip)
                    Build (sonic) / BuildDirect                                             (backup / http-only)
cmd/bench/        throughput binary for the cfast/in-pod comparison (ignore for opt work)
cmd/orchestrate/  paced load driver: sends 1KB pairs at RATE/sec for DURATION, writes cpu.prof
```
Module `goapproach`. Fixtures at repo-root `../fixtures` — `req-<size>.bin`/`resp-<size>.bin` are
COMPLETE raw HTTP messages (headers + body), built from `../json-files/*.json` (bodies only) by
`../gen-fixtures.py`. Sizes: 256b/1kb/4kb/10kb/20kb/64kb.

## How to measure (this is how you prove an improvement)

```bash
go test ./... -count=1                                         # correctness — MUST stay green
go test ./httpparser/ -run=^$ -fuzz=FuzzParseRequest -fuzztime=30s   # no-panic hunt (also FuzzParseResponse)
go test ./httpparser/ -bench=. -benchmem -run=^$               # parser: ns/op, M/s, allocs/op
go test ./kafkashape/ -bench=. -benchmem -run=^$               # encoder
go test ./httpparser/ -bench=ParseRequest/1kb$ -count=10 | tee new.txt
benchstat old.txt new.txt                                      # statistically sound before/after
```

The correctness gate is strong — treat a green `go test ./...` as the bar any change must clear:
- **Differential vs `net/http`** on the real fixtures (`TestFixturesMatchNetHTTP`) and on the
  full Kafka output (`TestBuildFullVsNetHTTP`) — your parse must agree with the stdlib.
- **Generative** — 2000 random well-formed requests checked field-by-field (`TestParserGenerative`).
- **Fuzz** — `FuzzParseRequest/Response` assert the parser NEVER panics on arbitrary bytes.
- **Edge cases** — no-headers, empty/tab values, duplicates, no-reason status, CRLF-in-body,
  >128 headers (`TestEdgeCases`). Run the fuzzer after any change to the scan loop.
Benchmark naming: `BenchmarkX` = single goroutine; `BenchmarkXParallel` = all cores
(`RunParallel`). Columns: `ns/op` (latency, noisy — use `-count=10`+benchstat), `M/s`
(ops/sec via ReportMetric), `allocs/op` (**stable — the primary signal**).

## Baseline to beat (Apple M-series, arm64; numbers move with load — allocs don't)

| Benchmark (1 KB) | ns/op | M/s | allocs/op |
|---|---|---|---|
| ParseRequest (1 core) | ~70 | ~14 | **0** |
| ParseRequestParallel (10 cores) | ~11 | ~88 | **0** |
| BuildFull (1 core) | ~2500 | ~0.4 | **0** |
| BuildFullParallel (10 cores) | ~370 | ~2.7 | **0** |

Parser is flat across body sizes (it doesn't scan the body). BuildFull grows with body
size because it must copy both bodies into the JSON output (unavoidable).

## Invariants — do NOT break these (tests enforce them)

1. **0 allocations on the hot path.** `ParseRequest/Response` and `BuildFull`/`BuildDirect`
   must stay `0 B/op, 0 allocs/op`. Verify with `-benchmem`.
2. **Safe on malformed input** — return `ErrMalformed`, never panic/crash. (`TestMalformedReturnsError`.)
3. **Zero-copy** — parsed fields are `[]byte` slices aliasing the input buffer; don't copy them.
4. **Always-valid JSON**, including binary/non-UTF-8 bodies: invalid UTF-8 → `U+FFFD`,
   and `"` `\` control chars escaped. (`TestEscapingAndInvalidUTF8`.)
5. **Output key contract** — all current keys; header values as nested objects; other
   values as strings; duplicate header names preserved; empty reason → no trailing space.
   (`TestBuildFull`, `TestEmptyReasonNoTrailingSpace`.)
6. **Scope: one complete message per buffer.** No streaming/chunked/pipelined framing.
7. **Reuse `Parser`/`Builder` per goroutine; never share across goroutines** (mutable scratch).

## Dead ends already explored (don't repeat)

- **sonic / encoding/json** — slower here. The cost is materializing maps + strings
  (~44 allocs), not the marshal step; SIMD encoders don't help. `Build` (sonic) kept only
  as a backup. The hand-rolled `BuildFull` (0-alloc) already won.
- **cgo to a C parser** — the ~40–250 ns cgo boundary per call erases the win; only helps
  if batching thousands of messages per call.
- **SWAR hand-scan** — ~15% over `bytes.IndexByte`, not worth it.
- **Custom SIMD** — Go can't inline assembly, so any SIMD is a non-inlinable `CALL`;
  `bytes.IndexByte` (already SIMD, runtime-dispatched on amd64/arm64) is the practical primitive.

## Where the time goes now (ideas, unverified)

- Parser: ~80% is delimiter scanning via `bytes.IndexByte` (per header line + colon).
  Fewer/cheaper scans, or better bounds-check elimination, are the levers.
- BuildFull: the body copy into the output dominates for large bodies; the header/envelope
  writing dominates for small ones. `appendJSONEscaped` is the inner loop — its fast path
  (valid ASCII/UTF-8) matters most.

Profile before optimizing:
- Micro (pipeline only): `go test ./kafkashape/ -bench=BuildFull/1kb$ -cpuprofile=/tmp/c.prof && go tool pprof -top /tmp/c.prof`
- Realistic load: `DURATION=30s RATE=30000 go run ./cmd/orchestrate` → `cpu.prof`, then `go tool pprof -http=: cpu.prof`.

Reality check on scale: at the production rate of ~30k pairs/sec the pipeline is **~2% of CPU** —
the box is otherwise idle. So micro-optimizing the parser has little end-to-end effect *at that rate*;
it matters only if the pair rate is far higher, or to cut allocations/GC. Optimize with that in mind:
prefer changes that reduce allocations (already 0 on the hot path — keep it) and that don't add risk.
