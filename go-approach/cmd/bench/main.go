// Command bench benchmarks the final zero-copy Go parser (httpparser) across
// every body size, requests and responses, single-core then all-cores.
// Output format matches cfast so the two can be compared side by side.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"goapproach/httpparser"
)

var sizes = []string{"256b", "1kb", "4kb", "10kb", "20kb", "64kb"}

const (
	dur  = 800 * time.Millisecond
	runs = 3
)

var sink uint64

func fixturesDir() string {
	if d := os.Getenv("FIXTURES"); d != "" {
		return d
	}
	return "fixtures"
}

func mustRead(dir, name string) []byte {
	b, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot read fixture:", err)
		os.Exit(1)
	}
	return b
}

type makeOp func() func() int

// benchN runs `workers` goroutines each with its own op for `dur`; returns
// aggregate parses/sec.
func benchN(mk makeOp, workers int) float64 {
	w := mk()
	for i := 0; i < 200_000; i++ {
		sink += uint64(w())
	}
	counts := make([]int64, workers)
	var wg sync.WaitGroup
	start := time.Now()
	for id := 0; id < workers; id++ {
		op := mk()
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			var n int64
			var s uint64
			for {
				for i := 0; i < 50_000; i++ {
					s += uint64(op())
				}
				n += 50_000
				if time.Since(start) >= dur {
					break
				}
			}
			counts[id] = n
			atomic.AddUint64(&sink, s)
		}(id)
	}
	wg.Wait()
	elapsed := time.Since(start)
	var total int64
	for _, c := range counts {
		total += c
	}
	return float64(total) / elapsed.Seconds()
}

func threeRuns(mk makeOp, workers int) [runs]float64 {
	var out [runs]float64
	for i := 0; i < runs; i++ {
		out[i] = benchN(mk, workers) / 1e6
	}
	return out
}

func reqOp(buf []byte) makeOp {
	return func() func() int {
		p := httpparser.New()
		return func() int {
			r, err := p.ParseRequest(buf)
			if err != nil {
				return 0
			}
			return len(r.Headers) + len(r.Body)
		}
	}
}
func respOp(buf []byte) makeOp {
	return func() func() int {
		p := httpparser.New()
		return func() int {
			r, err := p.ParseResponse(buf)
			if err != nil {
				return 0
			}
			return r.StatusCode + len(r.Body)
		}
	}
}

func runPass(dir string, workers int) {
	fmt.Println("  Go zero-copy parser (httpparser)")
	fmt.Printf("  %-6s %-5s | %9s %9s %9s | %9s\n", "body", "kind", "run1", "run2", "run3", "avg")
	fmt.Println("  ------------+-------------------------------+----------")
	for _, s := range sizes {
		req := mustRead(dir, "req-"+s+".bin")
		resp := mustRead(dir, "resp-"+s+".bin")
		printRow(s, "req", threeRuns(reqOp(req), workers))
		printRow(s, "resp", threeRuns(respOp(resp), workers))
	}
}

func printRow(size, kind string, r [runs]float64) {
	fmt.Printf("  %-6s %-5s | %9.2f %9.2f %9.2f | %9.2f\n",
		size, kind, r[0], r[1], r[2], (r[0]+r[1]+r[2])/3)
}

func main() {
	dir := fixturesDir()
	ncpu := runtime.NumCPU()
	fmt.Printf("Go parser benchmark — %s/%s, %d CPUs, %s   [M/s]\n",
		runtime.GOOS, runtime.GOARCH, ncpu, runtime.Version())
	fmt.Printf("\n========================= SINGLE CORE (1 goroutine) =========================\n")
	runPass(dir, 1)
	fmt.Printf("\n========================= ALL CORES (%d goroutines) =========================\n", ncpu)
	runPass(dir, ncpu)
	fmt.Fprintf(os.Stderr, "(sink=%d)\n", sink)
}
