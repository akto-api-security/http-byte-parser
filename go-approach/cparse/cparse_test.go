//go:build cgo && arm64
package cparse

import (
	"bufio"
	"bytes"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var sizes = []string{"256b", "1kb", "4kb", "10kb", "20kb", "64kb"}

func load(t testing.TB, name string) []byte {
	b, err := os.ReadFile(filepath.Join("..", "..", "fixtures", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// Same oracle as the Go parser's suite: net/http. We run BOTH C parsers through
// the identical Go assertions — one harness, no per-language drift.
type impl struct {
	name string
	req  func([]byte) Parsed
	resp func([]byte) Parsed
}

func impls() []impl {
	return []impl{
		{"cfast-scalar", ScalarRequest, ScalarResponse},
		{"cfast-simd", SimdRequest, SimdResponse},
	}
}

func lower(hs []Header) map[string]string {
	m := make(map[string]string, len(hs))
	for _, h := range hs {
		m[strings.ToLower(h.Name)] = h.Value
	}
	return m
}

func TestCParsersMatchNetHTTP(t *testing.T) {
	for _, im := range impls() {
		for _, s := range sizes {
			// request
			raw := load(t, "req-"+s+".bin")
			std, err := http.ReadRequest(bufio.NewReader(bytes.NewReader(raw)))
			if err != nil {
				t.Fatalf("%s: net/http rejected fixture: %v", s, err)
			}
			got := im.req(raw)
			stdBody, _ := io.ReadAll(std.Body)
			if got.Body != string(stdBody) {
				t.Errorf("%s/%s req body mismatch (got %d want %d)", im.name, s, len(got.Body), len(stdBody))
			}
			for name, val := range lower(got.Headers) {
				if name == "host" {
					if val != std.Host {
						t.Errorf("%s/%s host: got %q want %q", im.name, s, val, std.Host)
					}
					continue
				}
				if want := std.Header.Get(name); want != val {
					t.Errorf("%s/%s header %q: got %q want %q", im.name, s, name, val, want)
				}
			}

			// response
			rraw := load(t, "resp-"+s+".bin")
			rstd, err := http.ReadResponse(bufio.NewReader(bytes.NewReader(rraw)), nil)
			if err != nil {
				t.Fatalf("%s: net/http rejected resp fixture: %v", s, err)
			}
			rgot := im.resp(rraw)
			rBody, _ := io.ReadAll(rstd.Body)
			if rgot.Body != string(rBody) {
				t.Errorf("%s/%s resp body mismatch", im.name, s)
			}
			for name, val := range lower(rgot.Headers) {
				if want := rstd.Header.Get(name); want != val {
					t.Errorf("%s/%s resp header %q: got %q want %q", im.name, s, name, val, want)
				}
			}
		}
	}
}

// cfast-simd must agree with cfast-scalar field-for-field (cross-check the two C impls).
func TestSimdMatchesScalar(t *testing.T) {
	for _, s := range sizes {
		req := load(t, "req-"+s+".bin")
		a, b := ScalarRequest(req), SimdRequest(req)
		if a.Body != b.Body || len(a.Headers) != len(b.Headers) {
			t.Fatalf("%s req: scalar vs simd diverge", s)
		}
		for i := range a.Headers {
			if a.Headers[i] != b.Headers[i] {
				t.Fatalf("%s req header %d: scalar %v simd %v", s, i, a.Headers[i], b.Headers[i])
			}
		}
	}
}
