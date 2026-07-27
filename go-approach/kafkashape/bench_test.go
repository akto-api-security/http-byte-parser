package kafkashape

import (
	"sort"
	"testing"
)

// ---- bench-only helpers ----

func mps(b *testing.B, ops int64) {
	b.ReportMetric(float64(ops)/b.Elapsed().Seconds()/1e6, "M/s")
}

// gibps reports throughput of raw input bytes processed, in GiB/s.
func gibps(b *testing.B, ops, bytesPerOp int64) {
	b.ReportMetric(float64(ops*bytesPerOp)/b.Elapsed().Seconds()/(1<<30), "GiB/s")
}

// encoderKinds returns the registry keys in a stable order.
func encoderKinds() []string {
	ks := make([]string, 0, len(Encoders))
	for k := range Encoders {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

// ---- benchmarks (report both M/s and GiB/s; parse-once, encode-only) ----

// BenchmarkEncode: every encoder × size on a SINGLE core.
func BenchmarkEncode(b *testing.B) {
	m := sampleMeta()
	for _, kind := range encoderKinds() {
		for _, s := range sizes {
			req, resp, inBytes := parsePair(b, s)
			enc := NewEncoder(kind)
			enc.Encode(req, resp, m) // warm reused buffers
			b.Run(kind+"/"+s, func(b *testing.B) {
				b.ReportAllocs()
				var n int64
				for i := 0; i < b.N; i++ {
					_ = len(enc.Encode(req, resp, m))
					n++
				}
				mps(b, n)
				gibps(b, n, inBytes)
			})
		}
	}
}

// BenchmarkEncodeParallel: the same matrix across ALL cores. Each goroutine has
// its own encoder (encoders are not concurrency-safe); parsed structs + Meta are
// read-only and shared.
func BenchmarkEncodeParallel(b *testing.B) {
	m := sampleMeta()
	for _, kind := range encoderKinds() {
		for _, s := range sizes {
			req, resp, inBytes := parsePair(b, s)
			b.Run(kind+"/"+s, func(b *testing.B) {
				b.ReportAllocs()
				b.RunParallel(func(pb *testing.PB) {
					enc := NewEncoder(kind) // one per goroutine
					for pb.Next() {
						_ = len(enc.Encode(req, resp, m))
					}
				})
				mps(b, int64(b.N))
				gibps(b, int64(b.N), inBytes)
			})
		}
	}
}
