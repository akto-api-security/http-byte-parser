//go:build cgo && arm64

package cparse

import (
	"bytes"
	"testing"

	"goapproach/kafkashape"
)

// SAMPLE meta baked into cshape_shim.c — must match for byte-identical output.
func sampleMeta() *kafkashape.Meta {
	return &kafkashape.Meta{
		SourceIP: "10.0.0.1", DestIP: "10.0.0.2", TimeUnix: 1690000000,
		AktoAccountID: "1000000", VxlanID: 42, IsPending: false, Source: "MIRRORING",
		Direction: 1, ProcessID: 1234, SocketID: 7, DaemonsetID: "ds-1",
		ProcessName: "svc", EnableGraph: true, Tag: `{"env":"prod"}`,
	}
}

// cshape (C) must produce byte-identical output to kafkashape.Encode (Go).
func TestCShapeMatchesGoEncode(t *testing.T) {
	m := sampleMeta()
	bld := kafkashape.NewBuilder()
	p := bld.Parser()

	check := func(name string, reqB, respB []byte) {
		req, err := p.ParseRequest(reqB)
		if err != nil {
			t.Fatalf("%s: go ParseRequest: %v", name, err)
		}
		resp, err := p.ParseResponse(respB)
		if err != nil {
			t.Fatalf("%s: go ParseResponse: %v", name, err)
		}
		want := append([]byte(nil), bld.Encode(req, resp, m)...) // copy before cgo reuse
		got := CShapeEncode(reqB, respB)
		if !bytes.Equal(want, got) {
			t.Fatalf("%s: cshape != Go Encode\n go: %s\n  c: %s", name, want, got)
		}
	}

	for _, s := range sizes {
		check(s, load(t, "req-"+s+".bin"), load(t, "resp-"+s+".bin"))
	}
	// escaping + invalid UTF-8 + control chars
	check("escaping",
		[]byte("POST /p HTTP/1.1\r\nX-Weird: v\"a\\l\tue\r\nContent-Length: 9\r\n\r\na\"b\\c\nd\x01e\xfff"),
		[]byte("HTTP/1.1 200 OK\r\nContent-Length: 0\r\n\r\n"))
	// empty reason (204)
	check("empty-reason",
		[]byte("GET / HTTP/1.1\r\nHost: x\r\n\r\n"),
		[]byte("HTTP/1.1 204 \r\nContent-Length: 0\r\n\r\n"))
}
