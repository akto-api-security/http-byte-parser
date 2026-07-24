package kafkashape

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/bytedance/sonic"
)

var sizes = []string{"256b", "1kb", "4kb", "10kb", "20kb", "64kb"}

func load(t testing.TB, name string) []byte {
	b, err := os.ReadFile(filepath.Join("..", "..", "fixtures", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestBuild(t *testing.T) {
	b := NewBuilder()
	out, err := b.Build(load(t, "req-1kb.bin"), load(t, "resp-1kb.bin"))
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := sonic.Unmarshal(out, &got); err != nil {
		t.Fatalf("output not valid JSON: %v", err)
	}
	if got["method"] != "POST" || got["statusCode"] != "200" {
		t.Fatalf("bad payload: %v", got["method"])
	}
	rh := got["requestHeaders"].(map[string]any)
	if rh["Host"] != "localhost:8888" {
		t.Fatalf("bad request header: %v", rh["Host"])
	}
	t.Logf("output bytes: %d", len(out))
}

// Full pipeline: parse req+resp -> build struct -> sonic.Marshal.
func BenchmarkBuild(b *testing.B) {
	for _, s := range sizes {
		req := load(b, "req-"+s+".bin")
		resp := load(b, "resp-"+s+".bin")
		bld := NewBuilder()
		b.Run(s, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				out, err := bld.Build(req, resp)
				if err != nil {
					b.Fatal(err)
				}
				_ = len(out)
			}
		})
	}
}
