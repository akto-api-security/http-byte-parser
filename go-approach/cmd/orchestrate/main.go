// Command orchestrate drives the full parse+shape pipeline at a fixed rate of
// 30,000 (request,response) pairs/sec using 1 KB-body fixtures, while writing a
// CPU profile to cpu.prof. This shows how much CPU the pipeline actually costs at
// production load (not peak throughput).
//
//	go run ./cmd/orchestrate                 # 10s run, writes ./cpu.prof
//	DURATION=30s RATE=30000 go run ./cmd/orchestrate
//	go tool pprof -http=: cpu.prof
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime/pprof"
	"strconv"
	"time"

	"goapproach/kafkashape"
)

func fixturesDir() string {
	if d := os.Getenv("FIXTURES"); d != "" {
		return d
	}
	// works both from repo root (./bin/orchestrate) and from go-approach/ (go run)
	for _, c := range []string{"fixtures", filepath.Join("..", "fixtures")} {
		if _, err := os.Stat(filepath.Join(c, "req-1kb.bin")); err == nil {
			return c
		}
	}
	return "fixtures"
}

func read(name string) []byte {
	b, err := os.ReadFile(filepath.Join(fixturesDir(), name))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	return b
}

func envInt(k string, def int) int {
	if v := os.Getenv(k); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func envDur(k string, def time.Duration) time.Duration {
	if v := os.Getenv(k); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}

func main() {
	rate := envInt("RATE", 30000) // pairs/sec
	dur := envDur("DURATION", 10*time.Second)

	req := read("req-1kb.bin")
	resp := read("resp-1kb.bin")
	meta := &kafkashape.Meta{
		SourceIP: "10.0.0.1", DestIP: "10.0.0.2", TimeUnix: 1690000000,
		AktoAccountID: "1000000", VxlanID: 42, Source: "MIRRORING", Direction: 1,
	}
	b := kafkashape.NewBuilder()

	f, err := os.Create("cpu.prof")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer f.Close()
	if err := pprof.StartCPUProfile(f); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer pprof.StopCPUProfile()

	fmt.Printf("driving %d pairs/sec of 1KB bodies for %s (profiling -> cpu.prof)\n", rate, dur)

	var done, failed int64
	var sink int
	start := time.Now()
	end := start.Add(dur)
	for {
		now := time.Now()
		if !now.Before(end) {
			break
		}
		// catch up to the number of pairs we should have processed by now.
		should := int64(now.Sub(start).Seconds() * float64(rate))
		for done < should {
			out, err := b.BuildFull(req, resp, meta)
			if err != nil {
				failed++
			} else {
				sink += len(out) // in prod: go ProduceStr(ctx, string(out), ...)
			}
			done++
		}
		time.Sleep(100 * time.Microsecond) // yield so we pace instead of busy-spin
	}

	elapsed := time.Since(start)
	fmt.Printf("done: %d pairs in %s = %.0f pairs/sec (%d failed)\n",
		done, elapsed.Round(time.Millisecond), float64(done)/elapsed.Seconds(), failed)
	fmt.Printf("wrote cpu.prof — inspect with:  go tool pprof -top cpu.prof\n")
	_ = sink
}
