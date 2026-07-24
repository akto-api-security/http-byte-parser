package kafkashape
import ("testing";"github.com/bytedance/sonic")
func TestBuildDirectMatches(t *testing.T){
	b := NewBuilder()
	out, err := b.BuildDirect(load(t,"req-1kb.bin"), load(t,"resp-1kb.bin"))
	if err != nil { t.Fatal(err) }
	var got map[string]any
	if err := sonic.Unmarshal(out, &got); err != nil { t.Fatalf("invalid JSON: %v\n%s", err, out) }
	if got["method"]!="POST" || got["statusCode"]!="200" || got["status"]!="200 OK" { t.Fatalf("bad: %v", got) }
	rh := got["requestHeaders"].(map[string]any)
	if rh["Host"]!="localhost:8888" || rh["Content-Length"]!="1021" { t.Fatalf("bad headers: %v", rh) }
	if got["requestPayload"].(string)[:1] != "[" { t.Fatalf("bad body") }
}
