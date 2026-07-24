package kafkashape
import ("testing";"github.com/bytedance/sonic")
func sampleMeta() *Meta {
	return &Meta{SourceIP:"10.0.0.1", DestIP:"10.0.0.2", TimeUnix:1690000000,
		AktoAccountID:"1000000", VxlanID:42, IsPending:false, Source:"MIRRORING",
		Direction:1, ProcessID:1234, SocketID:7, DaemonsetID:"ds-1",
		ProcessName:"svc", EnableGraph:true, Tag:`{"env":"prod"}`}
}
func TestBuildFull(t *testing.T){
	b := NewBuilder()
	out, err := b.BuildFull(load(t,"req-1kb.bin"), load(t,"resp-1kb.bin"), sampleMeta())
	if err != nil { t.Fatal(err) }
	var g map[string]any
	if err := sonic.Unmarshal(out, &g); err != nil { t.Fatalf("invalid JSON: %v\n%s", err, out) }
	want := []string{"method","path","type","statusCode","status","requestHeaders",
		"responseHeaders","requestPayload","responsePayload","ip","destIp","time",
		"akto_account_id","akto_vxlan_id","is_pending","source","direction",
		"process_id","socket_id","daemonset_id","process_name","enable_graph","tag"}
	for _, k := range want { if _, ok := g[k]; !ok { t.Fatalf("missing key %q", k) } }
	if g["method"]!="POST" || g["statusCode"]!="200" || g["status"]!="200 OK" { t.Fatalf("bad vals: %v %v %v", g["method"],g["statusCode"],g["status"]) }
	if g["ip"]!="10.0.0.1" || g["akto_vxlan_id"]!="42" || g["is_pending"]!="false" || g["enable_graph"]!="true" { t.Fatalf("bad meta") }
	if g["tag"]!=`{"env":"prod"}` { t.Fatalf("bad tag: %v", g["tag"]) }
	t.Logf("keys=%d bytes=%d", len(g), len(out))
}
func mps(b *testing.B, ops int64){ b.ReportMetric(float64(ops)/b.Elapsed().Seconds()/1e6, "M/s") }

// SINGLE-CORE, per size.
func BenchmarkBuildFull(b *testing.B){
	m := sampleMeta()
	for _, s := range sizes {
		req, resp := load(b,"req-"+s+".bin"), load(b,"resp-"+s+".bin")
		bld := NewBuilder()
		b.Run(s, func(b *testing.B){
			b.ReportAllocs(); var n int64
			for b.Loop(){ out,err:=bld.BuildFull(req,resp,m); if err!=nil{b.Fatal(err)}; _=len(out); n++ }
			mps(b, n)
		})
	}
}

// ALL-CORES, per size (one Builder per goroutine — never share).
func BenchmarkBuildFullParallel(b *testing.B){
	m := sampleMeta()
	for _, s := range sizes {
		req, resp := load(b,"req-"+s+".bin"), load(b,"resp-"+s+".bin")
		b.Run(s, func(b *testing.B){
			b.ReportAllocs()
			b.RunParallel(func(pb *testing.PB){
				bld := NewBuilder()
				for pb.Next(){ out,err:=bld.BuildFull(req,resp,m); if err!=nil{b.Fatal(err)}; _=len(out) }
			})
			mps(b, int64(b.N))
		})
	}
}
