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

	capnp "capnproto.org/go/capnp/v3"
	"goapproach/httpparser"
)

// ---- shared helpers (used by correctness_test.go and bench_test.go) ----

var sizes = []string{"256b", "1kb", "4kb", "10kb", "20kb", "64kb"}

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

// encode parses reqBuf+respBuf and encodes with the JSON Builder (integration path).
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

// parsePair parses the fixture pair once and returns the parsed structs plus the
// total raw input bytes (req+resp message sizes) — the denominator for GiB/s.
func parsePair(t testing.TB, size string) (*httpparser.Request, *httpparser.Response, int64) {
	reqBuf, respBuf := load(t, "req-"+size+".bin"), load(t, "resp-"+size+".bin")
	p := httpparser.New()
	req, err := p.ParseRequest(reqBuf)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := p.ParseResponse(respBuf)
	if err != nil {
		t.Fatal(err)
	}
	return req, resp, int64(len(reqBuf) + len(respBuf))
}

// ---- JSON encoder: all keys + values present ----

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

// ---- binary formats: frame decodes back to the input fields ----

func TestRoundTrip(t *testing.T) {
	req, resp, _ := parsePair(t, "1kb")
	m := sampleMeta()

	t.Run("flatbuffers", func(t *testing.T) {
		out := NewFBEncoder().Encode(req, resp, m)
		fb := GetRootAsFbHttpPair(out, 0)
		if string(fb.MethodBytes()) != string(req.Method) ||
			string(fb.PathBytes()) != string(req.Path) ||
			int(fb.StatusCode()) != resp.StatusCode ||
			string(fb.ReqBodyBytes()) != string(req.Body) ||
			string(fb.RespBodyBytes()) != string(resp.Body) ||
			fb.ReqHeadersLength() != len(req.Headers) ||
			string(fb.SourceIp()) != m.SourceIP {
			t.Fatal("flatbuffers round-trip mismatch")
		}
	})

	t.Run("capnp", func(t *testing.T) {
		out := NewCapnpEncoder().Encode(req, resp, m)
		msg, err := capnp.Unmarshal(out)
		if err != nil {
			t.Fatal(err)
		}
		cp, err := ReadRootCpHttpPair(msg)
		if err != nil {
			t.Fatal(err)
		}
		method, _ := cp.Method()
		path, _ := cp.Path()
		rb, _ := cp.ReqBody()
		sb, _ := cp.RespBody()
		rh, _ := cp.ReqHeaders()
		sip, _ := cp.SourceIp()
		if string(method) != string(req.Method) ||
			string(path) != string(req.Path) ||
			int(cp.StatusCode()) != resp.StatusCode ||
			string(rb) != string(req.Body) ||
			string(sb) != string(resp.Body) ||
			rh.Len() != len(req.Headers) ||
			sip != m.SourceIP {
			t.Fatal("capnp round-trip mismatch")
		}
	})
}
