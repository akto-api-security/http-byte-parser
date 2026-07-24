#!/usr/bin/env python3
# Build byte-faithful HTTP request/response buffers for each JSON body size.
# Headers are exactly those from the user's original paste; Content-Length is real.
import os, pathlib

ROOT = pathlib.Path(__file__).parent
JSON = ROOT / "json-files"
OUT  = ROOT / "fixtures"
OUT.mkdir(exist_ok=True)

SIZES = ["256b", "1kb", "4kb", "10kb", "20kb", "64kb"]

def req(body: bytes) -> bytes:
    head = (
        "POST /?request-num=9 HTTP/1.1\r\n"
        "Host: localhost:8888\r\n"
        "User-Agent: traffic-gen/1.0\r\n"
        f"Content-Length: {len(body)}\r\n"
        "Accept: application/json\r\n"
        "Content-Type: application/json\r\n"
        "X-Debug-Token: test-req-buuf-test-runtime:24:07:26:02:30:55-request-9\r\n"
        "Accept-Encoding: gzip\r\n"
        "\r\n"
    ).encode("iso-8859-1")
    return head + body

def resp(body: bytes) -> bytes:
    head = (
        "HTTP/1.1 200 OK\r\n"
        "Accept: application/json\r\n"
        "Accept-Encoding: gzip\r\n"
        "Content-Type: application/json\r\n"
        "User-Agent: traffic-gen/1.0\r\n"
        "X-Debug-Token: test-req-buuf-test-runtime:24:07:26:02:30:55-request-9\r\n"
        "Date: Thu, 23 Jul 2026 21:00:55 GMT\r\n"
        f"Content-Length: {len(body)}\r\n"
        "\r\n"
    ).encode("iso-8859-1")
    return head + body

for s in SIZES:
    body = (JSON / f"{s}.json").read_bytes()
    (OUT / f"req-{s}.bin").write_bytes(req(body))
    (OUT / f"resp-{s}.bin").write_bytes(resp(body))
    print(f"{s}: body={len(body)}B  req={len(req(body))}B  resp={len(resp(body))}B")
