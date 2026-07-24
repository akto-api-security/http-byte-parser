#!/usr/bin/env bash
# Benchmark the Go parser (httpparser) and cfast (C), in one of three environments:
#
#   ./run.sh            # local  (default) — build & run natively, no Docker
#   ./run.sh local
#   ./run.sh docker     # build image, run in a container
#   ./run.sh k8s        # build+push multi-arch, deploy, exec in the pod
#
# Env overrides: IMAGE (docker/k8s), CC (local C compiler).
set -euo pipefail

MODE="${1:-local}"
HERE="$(cd "$(dirname "$0")" && pwd)"
cd "$HERE"
IMAGE="${IMAGE:-gauravakto/http-byte-parser:latest}"
NAME=httpparser-bench

gen_fixtures() { [ -f fixtures/req-1kb.bin ] || python3 gen-fixtures.py >/dev/null; }

run_local() {
  gen_fixtures
  local out; out="$(mktemp -d)"
  echo ">> building Go bench (pure Go) + cfast (C) ..."
  ( cd go-approach && CGO_ENABLED=0 go build -trimpath -o "$out/bench" ./cmd/bench )
  "${CC:-cc}" -O3 -pthread cfast/cfast.c -o "$out/cfast"
  echo ">> host: $(uname -sm), $(sysctl -n hw.logicalcpu 2>/dev/null || nproc) CPUs"
  echo
  FIXTURES="$HERE/fixtures" "$out/bench"
  FIXTURES="$HERE/fixtures" "$out/cfast"
  rm -rf "$out"
}

run_docker() {
  local img="${IMAGE##*/}"                 # use a local tag unless IMAGE was overridden
  [ "$IMAGE" = "gauravakto/http-byte-parser:latest" ] && img="httpparser-bench:latest" || img="$IMAGE"
  echo ">> building local image $img ..."
  docker build -q -t "$img" "$HERE" >/dev/null
  docker rm -f "$NAME" >/dev/null 2>&1 || true
  docker run -d --name "$NAME" "$img" >/dev/null
  echo ">> container $NAME up"
  docker exec "$NAME" /app/bench
  docker exec "$NAME" /app/cfast
}

run_k8s() {
  echo ">> building + pushing multi-arch $IMAGE (no QEMU) ..."
  docker buildx inspect xbuilder >/dev/null 2>&1 || docker buildx create --name xbuilder --driver docker-container --use
  docker buildx use xbuilder
  docker buildx build --platform linux/amd64,linux/arm64 -t "$IMAGE" --push "$HERE"
  echo ">> deploying ..."
  kubectl apply -f deployment.yaml
  kubectl rollout restart deploy/httpparser-bench
  kubectl rollout status deploy/httpparser-bench --timeout=120s
  local pod; pod="$(kubectl get pod -l app=httpparser-bench -o jsonpath='{.items[0].metadata.name}')"
  kubectl exec "$pod" -- /app/bench
  kubectl exec "$pod" -- /app/cfast
}

case "$MODE" in
  local)  run_local ;;
  docker) run_docker ;;
  k8s)    run_k8s ;;
  *) echo "usage: $0 [local|docker|k8s]" >&2; exit 2 ;;
esac
