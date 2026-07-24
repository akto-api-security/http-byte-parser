// Quick and dirty sonic benchmark: parse kafka.json in a for loop, time it.
package main

import (
	"fmt"
	"os"
	"time"

	"github.com/bytedance/sonic"
)

const runs = 100000

func main() {
	data, err := os.ReadFile("kafka.json")
	if err != nil {
		panic(err)
	}

	// warmup
	for i := 0; i < 1000; i++ {
		var v map[string]interface{}
		if err := sonic.Unmarshal(data, &v); err != nil {
			panic(err)
		}
	}

	start := time.Now()
	for i := 0; i < runs; i++ {
		var v map[string]interface{}
		if err := sonic.Unmarshal(data, &v); err != nil {
			panic(err)
		}
	}
	secs := time.Since(start).Seconds()

	perParseNs := secs * 1e9 / float64(runs)
	mbPerSec := (float64(len(data)) * float64(runs) / (1024.0 * 1024.0)) / secs
	parsesPerSec := float64(runs) / secs

	fmt.Printf("file: kafka.json (%d bytes)\n", len(data))
	fmt.Printf("runs: %d\n", runs)
	fmt.Printf("total: %.5f s\n", secs)
	fmt.Printf("per parse: %.1f ns\n", perParseNs)
	fmt.Printf("throughput: %.2f MB/s\n", mbPerSec)
	fmt.Printf("parses/sec: %.0f\n", parsesPerSec)
}
