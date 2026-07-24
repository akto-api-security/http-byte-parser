#!/usr/bin/env bash
# Build all parser binaries — Go + C variants.
#
#   ./build.sh            # local (default) — native binaries into ./bin
#   ./build.sh local
#   ./build.sh docker     # build the benchmark Docker image (Go bench + cfast)
#   ./build.sh all        # local + docker
#
# Env: CC (C compiler, default cc), IMAGE (docker tag, default httpparser-bench:latest)
set -euo pipefail

MODE="${1:-local}"
HERE="$(cd "$(dirname "$0")" && pwd)"
cd "$HERE"
CC="${CC:-cc}"
IMAGE="${IMAGE:-httpparser-bench:latest}"
BIN="$HERE/bin"

build_local() {
  mkdir -p "$BIN"
  echo ">> Go binaries (CGO off, pure Go) -> bin/"
  for cmd in bench orchestrate shapebench; do
    ( cd go-approach && CGO_ENABLED=0 go build -trimpath -o "$BIN/$cmd" "./cmd/$cmd" )
    echo "   built bin/$cmd"
  done

  echo ">> C binaries ($CC -O3) -> bin/"
  "$CC" -O3 -pthread cfast/cfast.c -o "$BIN/cfast"
  echo "   built bin/cfast"
  "$CC" -O3 -pthread pico-neon/bench_neon.c pico-neon/picohttpparser.c -o "$BIN/bench_neon"
  echo "   built bin/bench_neon"

  echo ">> done. binaries in $BIN:"
  ls -1 "$BIN"
}

build_docker() {
  echo ">> docker image $IMAGE (Go bench + cfast, per Dockerfile)"
  docker build -t "$IMAGE" "$HERE"
  echo ">> built image $IMAGE"
}

case "$MODE" in
  local)  build_local ;;
  docker) build_docker ;;
  all)    build_local; echo; build_docker ;;
  *) echo "usage: $0 [local|docker|all]" >&2; exit 2 ;;
esac
