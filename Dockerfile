# syntax=docker/dockerfile:1
# Multi-arch WITHOUT QEMU — benchmarks the Go parser (httpparser) and cfast (C).
#   - Go bench: CGO_ENABLED=0 (pure Go) -> cross by GOARCH only.
#   - cfast: pure C (memchr-based) -> cross-compiled with per-arch gcc.
# The build stage runs natively on $BUILDPLATFORM for every target.

FROM --platform=$BUILDPLATFORM golang:1.25-bookworm AS build
ARG TARGETOS
ARG TARGETARCH
RUN apt-get update && apt-get install -y --no-install-recommends \
      gcc-x86-64-linux-gnu gcc-aarch64-linux-gnu \
      libc6-dev-amd64-cross libc6-dev-arm64-cross \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /src
COPY go-approach/ ./go-approach/
COPY cfast/ ./cfast/

# 1) Go parser benchmark — pure Go.
WORKDIR /src/go-approach
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w" -o /out/bench ./cmd/bench

# 2) cfast benchmark — pure C, cross-compiled (relies on target libc memchr).
WORKDIR /src/cfast
RUN set -eu; \
    case "$TARGETARCH" in \
      amd64) CC=x86_64-linux-gnu-gcc ;; \
      arm64) CC=aarch64-linux-gnu-gcc ;; \
      *) echo "unsupported arch: $TARGETARCH" >&2; exit 1 ;; \
    esac; \
    "$CC" -O3 -pthread -I. common.c cfast.c -o /out/cfast
# (cfast-simd is arm64-only via NEON; the portable scalar cfast is what ships in the image)

FROM debian:bookworm-slim
WORKDIR /app
COPY --from=build /out/bench /app/bench
COPY --from=build /out/cfast /app/cfast
COPY fixtures/ /app/fixtures/
ENV FIXTURES=/app/fixtures
ENTRYPOINT ["sleep", "infinity"]
