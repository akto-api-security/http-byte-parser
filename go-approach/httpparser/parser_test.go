package httpparser

import (
	"os"
	"path/filepath"
	"testing"
)

var sizes = []string{"256b", "1kb", "4kb", "10kb", "20kb", "64kb"}

func load(t testing.TB, name string) []byte {
	// fixtures live at repo-root/fixtures; tests run from this package dir.
	b, err := os.ReadFile(filepath.Join("..", "..", "fixtures", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestParseRequest(t *testing.T) {
	p := New()
	r, err := p.ParseRequest(load(t, "req-1kb.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if string(r.Method) != "POST" || string(r.Path) != "/?request-num=9" || string(r.Version) != "HTTP/1.1" {
		t.Fatalf("bad line: %q %q %q", r.Method, r.Path, r.Version)
	}
	if len(r.Headers) != 7 {
		t.Fatalf("want 7 headers got %d", len(r.Headers))
	}
	if string(r.Headers[0].Name) != "Host" || string(r.Headers[0].Value) != "localhost:8888" {
		t.Fatalf("bad header0: %q=%q", r.Headers[0].Name, r.Headers[0].Value)
	}
	if len(r.Body) != 1021 {
		t.Fatalf("body len %d want 1021", len(r.Body))
	}
}

func TestParseResponse(t *testing.T) {
	p := New()
	r, err := p.ParseResponse(load(t, "resp-1kb.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if string(r.Version) != "HTTP/1.1" || r.StatusCode != 200 || string(r.Reason) != "OK" {
		t.Fatalf("bad status line: %q %d %q", r.Version, r.StatusCode, r.Reason)
	}
	if len(r.Headers) != 7 {
		t.Fatalf("want 7 headers got %d", len(r.Headers))
	}
}

func TestMalformedReturnsError(t *testing.T) {
	p := New()
	cases := [][]byte{
		[]byte("GET / garbage no newline"),
		[]byte("GET / HTTP/1.1\r\nBadHeaderNoColon\r\n\r\n"),
		[]byte("GET / HTTP/1.1\r\nHost: partial"), // no terminator
		[]byte(""),
	}
	for i, c := range cases {
		if _, err := p.ParseRequest(c); err == nil {
			t.Fatalf("case %d: expected error, got nil (would crash in prod)", i)
		}
	}
}

// mps adds an "M/s" (millions of ops/sec) column to a benchmark result.
func mps(b *testing.B, ops int64) {
	b.ReportMetric(float64(ops)/b.Elapsed().Seconds()/1e6, "M/s")
}

// --- SINGLE-CORE (one goroutine) ---

func BenchmarkParseRequest(b *testing.B) {
	for _, s := range sizes {
		buf := load(b, "req-"+s+".bin")
		p := New()
		b.Run(s, func(b *testing.B) {
			b.ReportAllocs()
			var n int64
			for b.Loop() {
				r, err := p.ParseRequest(buf)
				if err != nil {
					b.Fatal(err)
				}
				_ = len(r.Headers) + len(r.Body)
				n++
			}
			mps(b, n)
		})
	}
}

func BenchmarkParseResponse(b *testing.B) {
	for _, s := range sizes {
		buf := load(b, "resp-"+s+".bin")
		p := New()
		b.Run(s, func(b *testing.B) {
			b.ReportAllocs()
			var n int64
			for b.Loop() {
				r, err := p.ParseResponse(buf)
				if err != nil {
					b.Fatal(err)
				}
				_ = r.StatusCode + len(r.Body)
				n++
			}
			mps(b, n)
		})
	}
}

// --- ALL-CORES (GOMAXPROCS goroutines via RunParallel) ---

func BenchmarkParseRequestParallel(b *testing.B) {
	for _, s := range sizes {
		buf := load(b, "req-"+s+".bin")
		b.Run(s, func(b *testing.B) {
			b.ReportAllocs()
			b.RunParallel(func(pb *testing.PB) {
				p := New() // one parser per goroutine — never share
				for pb.Next() {
					r, err := p.ParseRequest(buf)
					if err != nil {
						b.Fatal(err)
					}
					_ = len(r.Headers) + len(r.Body)
				}
			})
			mps(b, int64(b.N)) // b.N = total iterations across all goroutines
		})
	}
}

func BenchmarkParseResponseParallel(b *testing.B) {
	for _, s := range sizes {
		buf := load(b, "resp-"+s+".bin")
		b.Run(s, func(b *testing.B) {
			b.ReportAllocs()
			b.RunParallel(func(pb *testing.PB) {
				p := New()
				for pb.Next() {
					r, err := p.ParseResponse(buf)
					if err != nil {
						b.Fatal(err)
					}
					_ = r.StatusCode + len(r.Body)
				}
			})
			mps(b, int64(b.N))
		})
	}
}
