package kafkashape

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

var sizes = []string{"256b", "1kb", "4kb", "10kb", "20kb", "64kb"}

// fixtures live at ebpf/testdata; this package is ebpf/httparser/kafkashape.
func load(t testing.TB, name string) []byte {
	b, err := os.ReadFile(filepath.Join("..", "..", "fixtures", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func sampleMeta() *Meta {
	return &Meta{
		SourceIP: "10.0.0.1", DestIP: "10.0.0.2", TimeUnix: 1690000000,
		AktoAccountID: "1000000", VxlanID: 42, IsPending: false, Source: "MIRRORING",
		Direction: 1, ProcessID: 1234, SocketID: 7, DaemonsetID: "ds-1",
		ProcessName: "svc", EnableGraph: true, Tag: `{"env":"prod"}`,
	}
}

func mps(b *testing.B, ops int64) { b.ReportMetric(float64(ops)/b.Elapsed().Seconds()/1e6, "M/s") }

// encode parses reqBuf+respBuf and encodes — the integration path.
func encode(t testing.TB, b *Builder, reqBuf, respBuf []byte, m *Meta) []byte {
	p := b.Parser()
	req, err := p.ParseRequest(reqBuf)
	if err != nil {
		t.Fatalf("ParseRequest: %v", err)
	}
	resp, err := p.ParseResponse(respBuf)
	if err != nil {
		t.Fatalf("ParseResponse: %v", err)
	}
	return b.Encode(req, resp, m)
}

// ---- all keys + values present ----

func TestEncode(t *testing.T) {
	out := encode(t, NewBuilder(), load(t, "req-1kb.bin"), load(t, "resp-1kb.bin"), sampleMeta())
	var g map[string]any
	if err := json.Unmarshal(out, &g); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	want := []string{"method", "path", "type", "statusCode", "status", "requestHeaders",
		"responseHeaders", "requestPayload", "responsePayload", "ip", "destIp", "time",
		"akto_account_id", "akto_vxlan_id", "is_pending", "source", "direction",
		"process_id", "socket_id", "daemonset_id", "process_name", "enable_graph", "tag"}
	for _, k := range want {
		if _, ok := g[k]; !ok {
			t.Fatalf("missing key %q", k)
		}
	}
	if g["method"] != "POST" || g["path"] != "/?request-num=9" || g["statusCode"] != "200" || g["status"] != "200 OK" {
		t.Fatalf("bad request/status line")
	}
	if g["requestHeaders"].(map[string]any)["Host"] != "localhost:8888" {
		t.Fatalf("bad Host header")
	}
	if g["ip"] != "10.0.0.1" || g["akto_vxlan_id"] != "42" || g["is_pending"] != "false" ||
		g["enable_graph"] != "true" || g["tag"] != `{"env":"prod"}` {
		t.Fatalf("bad metadata")
	}
}

// ---- differential vs net/http (the trusted oracle) across all sizes ----

func TestEncodeVsNetHTTP(t *testing.T) {
	b := NewBuilder()
	for _, s := range sizes {
		reqRaw, respRaw := load(t, "req-"+s+".bin"), load(t, "resp-"+s+".bin")
		stdReq, err := http.ReadRequest(bufio.NewReader(bytes.NewReader(reqRaw)))
		if err != nil {
			t.Fatalf("%s: net/http rejected fixture: %v", s, err)
		}
		stdReqBody, _ := io.ReadAll(stdReq.Body)
		stdResp, err := http.ReadResponse(bufio.NewReader(bytes.NewReader(respRaw)), nil)
		if err != nil {
			t.Fatalf("%s: net/http rejected resp: %v", s, err)
		}
		stdRespBody, _ := io.ReadAll(stdResp.Body)

		var g map[string]any
		if err := json.Unmarshal(encode(t, b, reqRaw, respRaw, &Meta{}), &g); err != nil {
			t.Fatalf("%s: invalid JSON: %v", s, err)
		}
		eq(t, s, "method", g["method"].(string), stdReq.Method)
		eq(t, s, "path", g["path"].(string), stdReq.RequestURI)
		eq(t, s, "statusCode", g["statusCode"].(string), strconv.Itoa(stdResp.StatusCode))
		eq(t, s, "requestPayload", g["requestPayload"].(string), string(stdReqBody))
		eq(t, s, "responsePayload", g["responsePayload"].(string), string(stdRespBody))
		checkHeaders(t, s+"/req", g["requestHeaders"].(map[string]any), stdReq.Header, stdReq.Host)
		checkHeaders(t, s+"/resp", g["responseHeaders"].(map[string]any), stdResp.Header, "")
	}
}

func eq(t *testing.T, size, key, got, want string) {
	if got != want {
		t.Errorf("%s %s: got %q want %q", size, key, got, want)
	}
}
func checkHeaders(t *testing.T, label string, got map[string]any, std http.Header, host string) {
	for name, v := range got {
		val := v.(string)
		if strings.EqualFold(name, "host") {
			if host != "" && val != host {
				t.Errorf("%s Host: got %q want %q", label, val, host)
			}
			continue
		}
		if want := std.Get(name); want != val {
			t.Errorf("%s header %q: got %q want %q", label, name, val, want)
		}
	}
}

// ---- escaping + invalid UTF-8 -> always valid JSON ----

func TestEscapingAndInvalidUTF8(t *testing.T) {
	body := "a\"b\\c\nd\x01e\xfff" // quotes, backslash, newline, control char, invalid utf-8 byte
	reqB := []byte("POST /p HTTP/1.1\r\nX-Weird: v\"a\\l\tue\r\nContent-Length: 9\r\n\r\n" + body)
	respB := []byte("HTTP/1.1 200 OK\r\nContent-Length: 0\r\n\r\n")
	out := encode(t, NewBuilder(), reqB, respB, &Meta{})
	var g map[string]any
	if err := json.Unmarshal(out, &g); err != nil {
		t.Fatalf("produced INVALID json: %v\n%s", err, out)
	}
	rp := g["requestPayload"].(string)
	if !strings.Contains(rp, "�") {
		t.Fatalf("invalid utf-8 not replaced with U+FFFD: %q", rp)
	}
	if !strings.Contains(rp, "a\"b\\c\nd") {
		t.Fatalf("escaping round-trip failed: %q", rp)
	}
	if g["requestHeaders"].(map[string]any)["X-Weird"] != "v\"a\\l\tue" {
		t.Fatalf("header escaping failed: %q", g["requestHeaders"].(map[string]any)["X-Weird"])
	}
}

func TestEmptyReasonNoTrailingSpace(t *testing.T) {
	out := encode(t, NewBuilder(),
		[]byte("GET / HTTP/1.1\r\nHost: x\r\n\r\n"),
		[]byte("HTTP/1.1 204 \r\nContent-Length: 0\r\n\r\n"), &Meta{})
	var g map[string]any
	json.Unmarshal(out, &g)
	if g["status"] != "204" {
		t.Fatalf("empty reason should give status %q, got %q", "204", g["status"])
	}
	if g["statusCode"] != "204" {
		t.Fatalf("statusCode should be %q, got %v", "204", g["statusCode"])
	}
}

// ---- benchmarks: full parse+encode, single-core and all-cores ----

func BenchmarkEncode(b *testing.B) {
	m := sampleMeta()
	for _, s := range sizes {
		reqB, respB := load(b, "req-"+s+".bin"), load(b, "resp-"+s+".bin")
		bld := NewBuilder()
		p := bld.Parser()
		b.Run(s, func(b *testing.B) {
			b.ReportAllocs()
			var n int64
			for i := 0; i < b.N; i++ {
				req, _ := p.ParseRequest(reqB)
				resp, _ := p.ParseResponse(respB)
				_ = len(bld.Encode(req, resp, m))
				n++
			}
			mps(b, n)
		})
	}
}

func BenchmarkEncodeParallel(b *testing.B) {
	m := sampleMeta()
	for _, s := range sizes {
		reqB, respB := load(b, "req-"+s+".bin"), load(b, "resp-"+s+".bin")
		b.Run(s, func(b *testing.B) {
			b.ReportAllocs()
			b.RunParallel(func(pb *testing.PB) {
				bld := NewBuilder()
				p := bld.Parser()
				for pb.Next() {
					req, _ := p.ParseRequest(reqB)
					resp, _ := p.ParseResponse(respB)
					_ = len(bld.Encode(req, resp, m))
				}
			})
			mps(b, int64(b.N))
		})
	}
}
