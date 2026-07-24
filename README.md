# http-byte-parser

Zero-copy HTTP `[]byte` → fields → Kafka JSON. Go (shipped) + C references.

```bash
python3 gen-fixtures.py          # once: build fixtures/ from json-files/
```

## Build

```bash
./build.sh                       # local (default): binaries -> ./bin
./build.sh docker                # benchmark image
./build.sh all                   # local + docker
```

## Run all benchmarks

```bash
./run.sh                         # local (default): Go + C
./run.sh docker                  # in a container
./run.sh k8s                     # build+push, deploy, exec in pod
```

## Run an individual C parser

```bash
FIXTURES=fixtures ./bin/cfast          # scalar memchr
FIXTURES=fixtures ./bin/cfast-simd     # NEON (arm64 only)
FIXTURES=fixtures ./bin/pico           # picohttpparser
FIXTURES=fixtures ./bin/cfast dump     # print parsed fields (eyeball check)
```

## Run the Go parser

```bash
FIXTURES=fixtures ./bin/bench                 # throughput (single + all cores)
DURATION=30s RATE=30000 ./bin/orchestrate     # paced load -> cpu.prof
```

## Run tests

```bash
cd go-approach
go test ./...                                            # correctness (arm64+cgo also drives the C parsers)
CGO_ENABLED=0 go test ./...                              # Go-only (skips C)
go test ./httpparser/ -bench=. -benchmem -run=^$         # parser throughput + allocs
go test ./httpparser/ -run=^$ -fuzz=FuzzParseRequest -fuzztime=30s   # fuzz
```
