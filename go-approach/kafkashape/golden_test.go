package kafkashape

import (
	"bufio"
	"bytes"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/bytedance/sonic"
)

// TestBuildFullVsNetHTTP: every HTTP-derived value BuildFull emits must match what
// net/http parses from the same bytes — a golden differential, not a spot-check.
func TestBuildFullVsNetHTTP(t *testing.T) {
	reqRaw := load(t, "req-1kb.bin")
	respRaw := load(t, "resp-1kb.bin")

	stdReq, err := http.ReadRequest(bufio.NewReader(bytes.NewReader(reqRaw)))
	if err != nil {
		t.Fatal(err)
	}
	stdReqBody, _ := io.ReadAll(stdReq.Body)
	stdResp, err := http.ReadResponse(bufio.NewReader(bytes.NewReader(respRaw)), nil)
	if err != nil {
		t.Fatal(err)
	}
	stdRespBody, _ := io.ReadAll(stdResp.Body)

	out, err := NewBuilder().BuildFull(reqRaw, respRaw, &Meta{})
	if err != nil {
		t.Fatal(err)
	}
	var g map[string]any
	if err := sonic.Unmarshal(out, &g); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	eq := func(key, got, want string) {
		if got != want {
			t.Errorf("%s: got %q want %q", key, got, want)
		}
	}
	eq("method", g["method"].(string), stdReq.Method)
	eq("path", g["path"].(string), stdReq.RequestURI)
	eq("type", g["type"].(string), stdReq.Proto)
	eq("statusCode", g["statusCode"].(string), strconv.Itoa(stdResp.StatusCode))
	eq("requestPayload", g["requestPayload"].(string), string(stdReqBody))
	eq("responsePayload", g["responsePayload"].(string), string(stdRespBody))

	checkHeaders(t, "requestHeaders", g["requestHeaders"].(map[string]any), stdReq.Header, stdReq.Host)
	checkHeaders(t, "responseHeaders", g["responseHeaders"].(map[string]any), stdResp.Header, "")

	// byte-stability: two builds of the same input produce identical bytes.
	out2, _ := NewBuilder().BuildFull(reqRaw, respRaw, &Meta{})
	if !bytes.Equal(out, out2) {
		t.Error("BuildFull is not deterministic across builders")
	}
}

func checkHeaders(t *testing.T, label string, got map[string]any, std http.Header, host string) {
	for name, v := range got {
		val := v.(string)
		if strings.EqualFold(name, "host") {
			if host != "" && val != host {
				t.Errorf("%s[Host]: got %q want %q", label, val, host)
			}
			continue
		}
		if want := std.Get(name); want != val {
			t.Errorf("%s[%s]: got %q want %q", label, name, val, want)
		}
	}
}
