package kafkashape
import ("strings";"testing";"github.com/bytedance/sonic")
func TestEscapingAndInvalidUTF8(t *testing.T){
	// body with quotes, backslash, newline, control char, and an INVALID utf-8 byte (0xFF)
	body := "a\"b\\c\nd\x01e\xfff"
	req := []byte("POST /p HTTP/1.1\r\nX-Weird: v\"a\\l\tue\r\nContent-Length: 9\r\n\r\n"+body)
	resp := []byte("HTTP/1.1 200 OK\r\nContent-Length: 0\r\n\r\n")
	b := NewBuilder()
	out, err := b.BuildDirect(req, resp)
	if err != nil { t.Fatal(err) }
	// must be VALID json despite invalid utf-8 + control chars in the input
	var g map[string]any
	if err := sonic.Unmarshal(out, &g); err != nil {
		t.Fatalf("produced INVALID json: %v\n%s", err, out)
	}
	rp := g["requestPayload"].(string)
	if !strings.Contains(rp, "�") { t.Fatalf("invalid utf-8 not replaced with U+FFFD: %q", rp) }
	if !strings.Contains(rp, "a\"b\\c\nd") { t.Fatalf("escaping round-trip failed: %q", rp) }
	rh := g["requestHeaders"].(map[string]any)
	if rh["X-Weird"] != "v\"a\\l\tue" { t.Fatalf("header escaping failed: %q", rh["X-Weird"]) }
}
func TestEmptyReasonNoTrailingSpace(t *testing.T){
	resp := []byte("HTTP/1.1 204 \r\nContent-Length: 0\r\n\r\n")
	req := []byte("GET / HTTP/1.1\r\nHost: x\r\n\r\n")
	b := NewBuilder()
	out, err := b.BuildDirect(req, resp)
	if err != nil { t.Fatal(err) }
	var g map[string]any
	sonic.Unmarshal(out, &g)
	if g["status"] != "204" { t.Fatalf("empty reason should give %q, got %q", "204", g["status"]) }
	if g["statusCode"] != "204" { t.Fatalf("statusCode should be string %q, got %v", "204", g["statusCode"]) }
}
