// Quick and dirty lazy sonic benchmark: uses sonic/ast to iterate top-level
// fields without materializing a map[string]interface{}, closer to simdjson's
// on-demand style.
package main

import (
	"fmt"
	"os"
	"time"

	"github.com/bytedance/sonic/ast"
)

const lazyRuns = 100000

func iterateOnce(data string) {
	root, err := ast.NewParser(data).Parse()
	if err != 0 {
		panic(err)
	}
	it, perr := root.Properties()
	if perr != nil {
		panic(perr)
	}
	var pair ast.Pair
	for it.Next(&pair) {
		_ = pair.Key
	}
}

func main() {
	raw, err := os.ReadFile("kafka.json")
	if err != nil {
		panic(err)
	}
	data := string(raw)

	// warmup
	for i := 0; i < 1000; i++ {
		iterateOnce(data)
	}

	start := time.Now()
	for i := 0; i < lazyRuns; i++ {
		iterateOnce(data)
	}
	secs := time.Since(start).Seconds()

	perParseNs := secs * 1e9 / float64(lazyRuns)
	mbPerSec := (float64(len(data)) * float64(lazyRuns) / (1024.0 * 1024.0)) / secs
	parsesPerSec := float64(lazyRuns) / secs

	fmt.Printf("file: kafka.json (%d bytes)\n", len(data))
	fmt.Printf("runs: %d\n", lazyRuns)
	fmt.Printf("total: %.5f s\n", secs)
	fmt.Printf("per parse: %.1f ns\n", perParseNs)
	fmt.Printf("throughput: %.2f MB/s\n", mbPerSec)
	fmt.Printf("parses/sec: %.0f\n", parsesPerSec)
}
