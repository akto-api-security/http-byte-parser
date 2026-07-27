// Command encprof runs a fixed number of encode iterations for one encoder over
// the 4kb fixture, writing CPU + heap (allocation) profiles for comparison.
//
//	go run ./cmd/encprof -kind flatbuffers -n 1000000 -out prof/flatbuffers
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"

	"goapproach/httpparser"
	"goapproach/kafkashape"
)

func main() {
	kind := flag.String("kind", "json", "encoder: json|flatbuffers|capnp")
	n := flag.Int("n", 1_000_000, "iterations")
	size := flag.String("size", "4kb", "fixture size")
	out := flag.String("out", "prof", "output dir")
	fixtures := flag.String("fixtures", "../fixtures", "fixtures dir")
	flag.Parse()

	must := func(err error) {
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	rd := func(name string) []byte {
		b, err := os.ReadFile(filepath.Join(*fixtures, name))
		must(err)
		return b
	}

	p := httpparser.New()
	req, err := p.ParseRequest(rd("req-" + *size + ".bin"))
	must(err)
	resp, err := p.ParseResponse(rd("resp-" + *size + ".bin"))
	must(err)

	m := &kafkashape.Meta{
		SourceIP: "10.0.0.1", DestIP: "10.0.0.2", TimeUnix: 1690000000,
		AktoAccountID: "1000000", VxlanID: 42, Source: "MIRRORING",
		Direction: 1, ProcessID: 1234, SocketID: 7, DaemonsetID: "ds-1",
		ProcessName: "svc", EnableGraph: true, Tag: `{"env":"prod"}`,
	}
	enc := kafkashape.NewEncoder(*kind)
	if enc == nil {
		must(fmt.Errorf("unknown encoder %q", *kind))
	}
	enc.Encode(req, resp, m) // warm

	must(os.MkdirAll(*out, 0o755))

	// Capture every allocation.
	runtime.MemProfileRate = 1

	cpuF, err := os.Create(filepath.Join(*out, "cpu.prof"))
	must(err)
	must(pprof.StartCPUProfile(cpuF))

	var sink int
	for i := 0; i < *n; i++ {
		sink += len(enc.Encode(req, resp, m))
	}
	pprof.StopCPUProfile()
	cpuF.Close()

	heapF, err := os.Create(filepath.Join(*out, "heap.prof"))
	must(err)
	must(pprof.WriteHeapProfile(heapF)) // includes alloc_space/alloc_objects
	heapF.Close()

	fmt.Printf("kind=%s size=%s n=%d sink=%d -> %s/{cpu,heap}.prof\n", *kind, *size, *n, sink, *out)
}
