package kafkashape
import "testing"
func TestProcessPairs(t *testing.T){
	pairs := []Pair{
		{Req: load(t,"req-1kb.bin"), Resp: load(t,"resp-1kb.bin")},
		{Req: []byte("bad"), Resp: []byte("bad")},          // malformed -> skipped
		{Req: load(t,"req-256b.bin"), Resp: load(t,"resp-256b.bin")},
	}
	out, st := ProcessPairs(pairs, &Meta{})
	if st.OK != 2 || st.Failed != 1 { t.Fatalf("OK=%d Failed=%d want 2/1", st.OK, st.Failed) }
	if len(out) != 2 { t.Fatalf("collected %d want 2", len(out)) }
	// copies must be independent (not aliasing one reused buffer)
	if string(out[0]) == string(out[1]) { t.Fatal("results should differ") }
	var n int
	st2 := ProcessPairsFunc(pairs, &Meta{}, func(js []byte){ n++ })
	if st2.OK != 2 || n != 2 { t.Fatalf("streaming OK=%d calls=%d", st2.OK, n) }
}
