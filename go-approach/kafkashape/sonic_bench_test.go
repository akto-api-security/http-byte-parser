package kafkashape

import (
	"testing"

	"github.com/bytedance/sonic"
	"goapproach/httpparser"
)

// sonicPayload mirrors the hand-rolled output shape so the comparison is fair:
// the same fields, headers as nested objects, values as strings.
type sonicPayload struct {
	Method          string            `json:"method"`
	Path            string            `json:"path"`
	Type            string            `json:"type"`
	StatusCode      string            `json:"statusCode"`
	Status          string            `json:"status"`
	RequestHeaders  map[string]string `json:"requestHeaders"`
	ResponseHeaders map[string]string `json:"responseHeaders"`
	RequestPayload  string            `json:"requestPayload"`
	ResponsePayload string            `json:"responsePayload"`
	IP              string            `json:"ip"`
	DestIP          string            `json:"destIp"`
	// (metadata trimmed to keep the struct short; body dominates cost anyway)
}

func toSonicPayload(req *httpparser.Request, resp *httpparser.Response, m *Meta) sonicPayload {
	rh := make(map[string]string, len(req.Headers))
	for _, h := range req.Headers {
		rh[string(h.Name)] = string(h.Value)
	}
	sh := make(map[string]string, len(resp.Headers))
	for _, h := range resp.Headers {
		sh[string(h.Name)] = string(h.Value)
	}
	return sonicPayload{
		Method: string(req.Method), Path: string(req.Path), Type: string(req.Version),
		StatusCode: "200", Status: "200 OK",
		RequestHeaders: rh, ResponseHeaders: sh,
		RequestPayload: string(req.Body), ResponsePayload: string(resp.Body),
		IP: m.SourceIP, DestIP: m.DestIP,
	}
}

// ---- full encode: hand-rolled Encode vs sonic (build struct + Marshal) ----

func BenchmarkSonicEncode(b *testing.B) {
	m := sampleMeta()
	for _, s := range sizes {
		reqB, respB := load(b, "req-"+s+".bin"), load(b, "resp-"+s+".bin")
		p := httpparser.New()
		req, _ := p.ParseRequest(reqB) // parse once — measure sonic encode only
		resp, _ := p.ParseResponse(respB)
		b.Run(s, func(b *testing.B) {
			b.ReportAllocs()
			var n int64
			for i := 0; i < b.N; i++ {
				pay := toSonicPayload(req, resp, m) // sonic's "encode" = build struct/maps + Marshal
				out, _ := sonic.Marshal(&pay)
				_ = len(out)
				n++
			}
			mps(b, n)
		})
	}
}

// ---- isolated escaper: the body-string escape that dominates at 4KB+ ----
// hand-rolled appendJSONString(body) vs sonic.Marshal(bodyString).

func benchBody(b *testing.B, size string) []byte {
	p := httpparser.New()
	req, err := p.ParseRequest(load(b, "req-"+size+".bin"))
	if err != nil {
		b.Fatal(err)
	}
	return append([]byte(nil), req.Body...) // detach from parser scratch
}

func BenchmarkEscape_HandRolled(b *testing.B) {
	for _, s := range sizes {
		body := benchBody(b, s)
		dst := make([]byte, 0, len(body)+16)
		b.Run(s, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(body)))
			for i := 0; i < b.N; i++ {
				_ = appendJSONString(dst[:0], body)
			}
		})
	}
}

func BenchmarkEscape_Sonic(b *testing.B) {
	for _, s := range sizes {
		body := string(benchBody(b, s))
		b.Run(s, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(body)))
			for i := 0; i < b.N; i++ {
				out, _ := sonic.Marshal(body) // marshals a Go string -> escaped JSON string
				_ = len(out)
			}
		})
	}
}
