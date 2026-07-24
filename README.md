# http-byte-parser

Zero-copy HTTP `[]byte` → structured fields → Kafka JSON, in Go (shipped) with C references.

## Quick start

```bash
python3 gen-fixtures.py        # generate fixtures/ from json-files/ (once)
./build.sh                     # build all Go + C binaries into ./bin
./run.sh                       # build + run the Go vs cfast benchmark locally

go test ./go-approach/...                                       # correctness
go test ./go-approach/httpparser/ -bench=. -benchmem -run=^$    # parser throughput + allocs
```

## Scripts

**`./build.sh [local|docker|all]`** — build binaries (default `local`).
- `local` — native binaries into `./bin/`: `bench`, `orchestrate`, `shapebench` (Go), `cfast`, `bench_neon` (C)
- `docker` — build the benchmark image (Go `bench` + `cfast`, cross-compiled)
- `all` — local + docker
- env: `CC` (C compiler), `IMAGE` (docker tag)

**`./run.sh [local|docker|k8s]`** — build + run the Go vs cfast benchmark (default `local`).
- `local` — native build + run, no Docker
- `docker` — build image, run in a container
- `k8s` — multi-arch build + push, deploy, exec in the pod
- env: `IMAGE`, `CC`

**Binaries in `./bin`** (after `build.sh`):
```bash
FIXTURES=fixtures ./bin/bench          # Go parser: single-core + all-cores, M/s
FIXTURES=fixtures ./bin/cfast          # C memchr parser
FIXTURES=fixtures ./bin/bench_neon     # picohttpparser NEON port
DURATION=30s RATE=30000 ./bin/orchestrate   # paced load driver -> cpu.prof
```
