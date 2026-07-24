// shapebench compares the two encoders on real fixtures, reporting ns/op,
// allocs/op, and B/op. Uses testing.Benchmark so it runs as a plain binary
// (no `go test` needed on the target host — handy for the amd64 cluster node).
//
//	sonic  = parse + build maps/struct + sonic.Marshal
//	direct = parse + hand-rolled append encoder (no maps, no sonic)
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"goapproach/kafkashape"
)

var sizes = []string{"256b", "1kb", "4kb", "10kb", "20kb", "64kb"}

func fixturesDir() string {
	if d := os.Getenv("FIXTURES"); d != "" {
		return d
	}
	return "fixtures"
}

func mustRead(name string) []byte {
	b, err := os.ReadFile(filepath.Join(fixturesDir(), name))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	return b
}

func main() {
	fmt.Printf("shape encoder benchmark — host %s/%s, %s\n\n", runtime.GOOS, runtime.GOARCH, runtime.Version())
	fmt.Printf("%-6s %-7s | %10s %10s %10s | %s\n", "body", "encoder", "ns/op", "allocs/op", "B/op", "M/s")
	fmt.Println("-------+---------+----------------------------------+------")

	for _, s := range sizes {
		req := mustRead("req-" + s + ".bin")
		resp := mustRead("resp-" + s + ".bin")

		bSonic := testing.Benchmark(func(b *testing.B) {
			bld := kafkashape.NewBuilder()
			b.ReportAllocs()
			for b.Loop() {
				out, err := bld.Build(req, resp)
				if err != nil {
					b.Fatal(err)
				}
				_ = len(out)
			}
		})
		bDirect := testing.Benchmark(func(b *testing.B) {
			bld := kafkashape.NewBuilder()
			b.ReportAllocs()
			for b.Loop() {
				out, err := bld.BuildDirect(req, resp)
				if err != nil {
					b.Fatal(err)
				}
				_ = len(out)
			}
		})
		row(s, "sonic", bSonic)
		row(s, "direct", bDirect)
	}
}

func row(size, name string, r testing.BenchmarkResult) {
	ns := float64(r.NsPerOp())
	fmt.Printf("%-6s %-7s | %10.1f %10d %10d | %6.2f\n",
		size, name, ns, r.AllocsPerOp(), r.AllocedBytesPerOp(), 1e9/ns/1e6)
}
