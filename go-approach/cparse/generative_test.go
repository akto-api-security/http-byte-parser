//go:build cgo && arm64

package cparse

import (
	"bufio"
	"bytes"
	"fmt"
	"math/rand"
	"net/http"
	"strings"
	"testing"
)

// Same idea as httpparser.TestParserGenerative, but pointed at the C parsers
// through the cgo shim: manufacture thousands of random well-formed requests and
// check both cfast and cfast-simd reproduce exactly the fields we generated
// (a perfect oracle), plus a net/http cross-check on the request line.
func TestCParsersGenerative(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	methods := []string{"GET", "POST", "PUT", "DELETE", "PATCH"}
	hdrNames := []string{"Accept", "User-Agent", "X-Trace", "X-Custom-Header", "Cookie", "Referer"}

	const iters = 2000
	for i := 0; i < iters; i++ {
		method := methods[rng.Intn(len(methods))]
		path := "/" + randTok(rng, 1+rng.Intn(20))
		if rng.Intn(2) == 0 {
			path += "?q=" + randTok(rng, rng.Intn(10))
		}
		host := randTok(rng, 3+rng.Intn(10)) + ":8080"
		body := randBody(rng, rng.Intn(200))

		want := map[string]string{"host": host}
		var sb strings.Builder
		fmt.Fprintf(&sb, "%s %s HTTP/1.1\r\nHost: %s\r\n", method, path, host)
		perm := rng.Perm(len(hdrNames))
		for j := 0; j < rng.Intn(len(hdrNames)+1); j++ {
			name := hdrNames[perm[j]]
			val := randVal(rng, 1+rng.Intn(30))
			fmt.Fprintf(&sb, "%s: %s\r\n", name, val)
			want[strings.ToLower(name)] = val
		}
		fmt.Fprintf(&sb, "Content-Length: %d\r\n\r\n", len(body))
		want["content-length"] = fmt.Sprintf("%d", len(body))
		sb.WriteString(body)
		raw := []byte(sb.String())

		for _, im := range impls() {
			got := im.req(raw)
			if got.Body != body {
				t.Fatalf("%s iter %d: body mismatch", im.name, i)
			}
			gm := lower(got.Headers)
			if len(gm) != len(want) {
				t.Fatalf("%s iter %d: header count got %d want %d\n%q", im.name, i, len(gm), len(want), raw)
			}
			for k, v := range want {
				if gm[k] != v {
					t.Fatalf("%s iter %d: header %q got %q want %q", im.name, i, k, gm[k], v)
				}
			}
		}
		// net/http cross-check on the request line
		if std, e := http.ReadRequest(bufio.NewReader(bytes.NewReader(raw))); e == nil {
			if std.Method != method || std.RequestURI != path {
				t.Fatalf("iter %d: net/http disagreed: %q %q", i, std.Method, std.RequestURI)
			}
		}
	}
}

func randTok(r *rand.Rand, n int) string {
	const cs = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_"
	b := make([]byte, n)
	for i := range b {
		b[i] = cs[r.Intn(len(cs))]
	}
	return string(b)
}
func randVal(r *rand.Rand, n int) string {
	const cs = "abcdefghijklmnopqrstuvwxyz0123456789/.:;=+()[]"
	b := make([]byte, n)
	for i := range b {
		b[i] = cs[r.Intn(len(cs))]
	}
	return string(b)
}
func randBody(r *rand.Rand, n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(32 + r.Intn(94))
	}
	return string(b)
}
