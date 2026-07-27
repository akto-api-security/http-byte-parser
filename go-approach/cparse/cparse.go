//go:build cgo && arm64
// Package cparse exposes the C parsers (cfast scalar + cfast-simd NEON) to Go via
// cgo, so the SAME Go correctness suite can be run against them — no separate C
// test harness, no chance of the two drifting apart.
//
// cgo is fine here because correctness doesn't care about the call overhead.
// NEVER benchmark through this package — the cgo boundary distorts a 20 ns parse.
// The C parsers do no validation, so this is for well-formed input only; feeding
// malformed bytes can crash the process (a C segfault is not a recoverable panic).
package cparse

/*
#cgo CFLAGS: -O3 -I${SRCDIR}/../../cfast
#cgo LDFLAGS: -pthread
#include "shim.h"
*/
import "C"
import "unsafe"

const maxH = 128

type Header struct{ Name, Value string }

// Parsed is the field-level result, decoded from the C parser's offsets. Strings
// are copies (correctness only), so no lifetime coupling to buf.
type Parsed struct {
	Headers []Header
	Body    string
	N       int // header count the parser reported (may exceed len(Headers) if capped)
}

type kind int

const (
	scalarReq kind = iota
	scalarResp
	simdReq
	simdResp
)

func run(k kind, buf []byte) Parsed {
	if len(buf) == 0 {
		return Parsed{}
	}
	no := make([]C.int, maxH)
	nl := make([]C.int, maxH)
	vo := make([]C.int, maxH)
	vl := make([]C.int, maxH)
	var bo, bl C.int
	p := (*C.char)(unsafe.Pointer(&buf[0]))
	ln := C.size_t(len(buf))
	var n C.int
	switch k {
	case scalarReq:
		n = C.cf_scalar_req(p, ln, &no[0], &nl[0], &vo[0], &vl[0], C.int(maxH), &bo, &bl)
	case scalarResp:
		n = C.cf_scalar_resp(p, ln, &no[0], &nl[0], &vo[0], &vl[0], C.int(maxH), &bo, &bl)
	case simdReq:
		n = C.cf_simd_req(p, ln, &no[0], &nl[0], &vo[0], &vl[0], C.int(maxH), &bo, &bl)
	case simdResp:
		n = C.cf_simd_resp(p, ln, &no[0], &nl[0], &vo[0], &vl[0], C.int(maxH), &bo, &bl)
	}

	m := int(n)
	if m > maxH {
		m = maxH
	}
	hs := make([]Header, m)
	for i := 0; i < m; i++ {
		hs[i] = Header{
			Name:  string(buf[no[i] : no[i]+nl[i]]),
			Value: string(buf[vo[i] : vo[i]+vl[i]]),
		}
	}
	return Parsed{Headers: hs, Body: string(buf[bo : bo+bl]), N: int(n)}
}

// Scalar = cfast.c (memchr). Simd = cfast-simd.c (NEON fused mask walker).
func ScalarRequest(buf []byte) Parsed  { return run(scalarReq, buf) }
func ScalarResponse(buf []byte) Parsed { return run(scalarResp, buf) }
func SimdRequest(buf []byte) Parsed    { return run(simdReq, buf) }
func SimdResponse(buf []byte) Parsed   { return run(simdResp, buf) }
