//go:build cgo && arm64

package cparse

// #include "shim.h"
import "C"
import "unsafe"

// CShapeEncode runs the C parser + C encoder (cshape) on a raw request/response
// pair and returns the Kafka JSON (a copy). Uses the fixed SAMPLE meta baked into
// the shim so it can be compared byte-for-byte against Go's kafkashape.Encode.
func CShapeEncode(reqBuf, respBuf []byte) []byte {
	if len(reqBuf) == 0 || len(respBuf) == 0 {
		return nil
	}
	var p *C.char
	n := C.cshape_encode_pair(
		(*C.char)(unsafe.Pointer(&reqBuf[0])), C.size_t(len(reqBuf)),
		(*C.char)(unsafe.Pointer(&respBuf[0])), C.size_t(len(respBuf)), &p)
	return C.GoBytes(unsafe.Pointer(p), n)
}
