// Command enctable prints an aligned throughput table (M/s + GiB/s, single-core
// and all-cores) for every registered kafkashape encoder × body size.
//
//	go run ./cmd/enctable            # fixtures at ../fixtures
//	FIXTURES=/path go run ./cmd/enctable
//
// It is NOT a test, so it never runs (or slows) `go test`.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"goapproach/httpparser"
	"goapproach/kafkashape"
)

var sizes = []string{"256b", "1kb", "4kb", "10kb", "20kb", "64kb"}

func fixturesDir() string {
	if d := os.Getenv("FIXTURES"); d != "" {
		return d
	}
	return "../fixtures"
}

func load(name string) []byte {
	b, err := os.ReadFile(filepath.Join(fixturesDir(), name))
	if err != nil {
		fmt.Fprintf(os.Stderr, "load %s: %v\n(set FIXTURES=<dir> if fixtures are elsewhere)\n", name, err)
		os.Exit(1)
	}
	return b
}

func sampleMeta() *kafkashape.Meta {
	return &kafkashape.Meta{
		SourceIP: "10.0.0.1", DestIP: "10.0.0.2", TimeUnix: 1690000000,
		AktoAccountID: "1000000", VxlanID: 42, IsPending: false, Source: "MIRRORING",
		Direction: 1, ProcessID: 1234, SocketID: 7, DaemonsetID: "ds-1",
		ProcessName: "svc", EnableGraph: true, Tag: `{"env":"prod"}`,
	}
}

func parsePair(size string) (*httpparser.Request, *httpparser.Response, int64) {
	reqBuf, respBuf := load("req-"+size+".bin"), load("resp-"+size+".bin")
	p := httpparser.New()
	req, err := p.ParseRequest(reqBuf)
	if err != nil {
		fmt.Fprintf(os.Stderr, "parse req %s: %v\n", size, err)
		os.Exit(1)
	}
	resp, err := p.ParseResponse(respBuf)
	if err != nil {
		fmt.Fprintf(os.Stderr, "parse resp %s: %v\n", size, err)
		os.Exit(1)
	}
	return req, resp, int64(len(reqBuf) + len(respBuf))
}

func kinds() []string {
	ks := make([]string, 0, len(kafkashape.Encoders))
	for k := range kafkashape.Encoders {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

func main() {
	m := sampleMeta()
	const row = "%-8s  %-11s  %-5s  %9s  %8s  %8s  %8s  %7s\n"
	fmt.Printf(row, "mode", "encoder", "size", "ns/op", "GiB/s", "M/s", "B/op", "allocs")

	report := func(mode, kind, size string, inBytes int64, r testing.BenchmarkResult) {
		secs := r.T.Seconds()
		gib := float64(inBytes) * float64(r.N) / secs / (1 << 30)
		mps := float64(r.N) / secs / 1e6
		// Fully formatted, aligned row printed immediately (streams live).
		fmt.Printf(row, mode, kind, size,
			fmt.Sprintf("%d", r.NsPerOp()),
			fmt.Sprintf("%.2f", gib),
			fmt.Sprintf("%.3f", mps),
			fmt.Sprintf("%d", r.AllocedBytesPerOp()),
			fmt.Sprintf("%d", r.AllocsPerOp()))
	}

	for _, kind := range kinds() {
		for _, s := range sizes {
			req, resp, inBytes := parsePair(s)
			enc := kafkashape.NewEncoder(kind)
			enc.Encode(req, resp, m) // warm
			report("single", kind, s, inBytes, testing.Benchmark(func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					_ = enc.Encode(req, resp, m)
				}
			}))
		}
	}
	for _, kind := range kinds() {
		for _, s := range sizes {
			req, resp, inBytes := parsePair(s)
			report("parallel", kind, s, inBytes, testing.Benchmark(func(b *testing.B) {
				b.ReportAllocs()
				b.RunParallel(func(pb *testing.PB) {
					enc := kafkashape.NewEncoder(kind)
					for pb.Next() {
						_ = enc.Encode(req, resp, m)
					}
				})
			}))
		}
	}
}
