// Package kafkashape turns raw (request, response) byte buffers into the Kafka
// JSON payload.
//
//   - BuildFull  — the production path: hand-rolled, zero-alloc, emits every key.
//   - BuildDirect — hand-rolled, HTTP-derived keys only.
//   - Build      — sonic + maps/struct; kept as a backup, not the hot path.
//
// All encoders escape JSON correctly, including repairing invalid UTF-8 (bodies
// may contain arbitrary bytes), so the output is always valid JSON.
//
// Concurrency: a Builder is NOT safe for concurrent use — it holds a reusable
// parser and output buffer. Use one Builder per goroutine (or a sync.Pool).
package kafkashape

import (
	"log"
	"strconv"
	"unicode/utf8"

	"github.com/bytedance/sonic"
	"goapproach/httpparser"
)

// Pair is one raw request+response to process (the bytes straight off the wire).
type Pair struct {
	Req  []byte
	Resp []byte
}

// Stats reports how a batch went.
type Stats struct {
	OK     int
	Failed int
}

// ProcessPairs is the batch orchestrator: for each (req, resp) pair it parses and
// builds the Kafka JSON, skipping malformed pairs (logged + counted), and returns
// a fresh copy of the JSON for each success. Uses a single Builder (this call is
// single-goroutine); use one ProcessPairs call per goroutine.
//
// Note: BuildFull returns a slice aliasing the Builder's internal buffer, so each
// result is COPIED before the next iteration overwrites it. For a produce-and-
// forget hot path, prefer ProcessPairsFunc to avoid retaining every payload.
func ProcessPairs(pairs []Pair, m *Meta) ([][]byte, Stats) {
	b := NewBuilder()
	out := make([][]byte, 0, len(pairs))
	var st Stats
	for i := range pairs {
		js, err := b.BuildFull(pairs[i].Req, pairs[i].Resp, m)
		if err != nil {
			st.Failed++
			log.Printf("kafkashape: skipping pair %d: %v", i, err)
			continue
		}
		out = append(out, append([]byte(nil), js...)) // copy: js aliases builder scratch
		st.OK++
	}
	return out, st
}

// ProcessPairsFunc is the streaming variant: it calls fn with each pair's Kafka
// JSON as it's produced (no copy, no retention — fn must consume/copy before
// returning, e.g. produce to Kafka or string(js)). Malformed pairs are skipped.
func ProcessPairsFunc(pairs []Pair, m *Meta, fn func(js []byte)) Stats {
	b := NewBuilder()
	var st Stats
	for i := range pairs {
		js, err := b.BuildFull(pairs[i].Req, pairs[i].Resp, m)
		if err != nil {
			st.Failed++
			log.Printf("kafkashape: skipping pair %d: %v", i, err)
			continue
		}
		fn(js)
		st.OK++
	}
	return st
}

// Payload is the backup (sonic) representation. StatusCode is a string to match
// the hand-rolled encoders and the legacy map[string]string data.
type Payload struct {
	Method          string            `json:"method"`
	Path            string            `json:"path"`
	Type            string            `json:"type"`
	StatusCode      string            `json:"statusCode"`
	Status          string            `json:"status"`
	RequestHeaders  map[string]string `json:"requestHeaders"`
	ResponseHeaders map[string]string `json:"responseHeaders"`
	RequestPayload  string            `json:"requestPayload"`
	ResponsePayload string            `json:"responsePayload"`
}

// Builder pairs a reusable Parser with a reusable output buffer. NOT safe for
// concurrent use; use one per goroutine.
type Builder struct {
	p       *httpparser.Parser
	payload Payload
	scratch []byte
}

func NewBuilder() *Builder {
	return &Builder{p: httpparser.New(), scratch: make([]byte, 0, 8192)}
}

// statusText returns "<code>" or "<code> <reason>" (no trailing space when the
// reason phrase is empty).
func statusText(code int, reason []byte) string {
	if len(reason) == 0 {
		return strconv.Itoa(code)
	}
	return strconv.Itoa(code) + " " + string(reason)
}

// Build (BACKUP, sonic). Returns freshly-allocated bytes, safe to retain.
func (b *Builder) Build(reqBuf, respBuf []byte) ([]byte, error) {
	req, err := b.p.ParseRequest(reqBuf)
	if err != nil {
		return nil, err
	}
	reqHeaders := make(map[string]string, len(req.Headers))
	for _, h := range req.Headers {
		reqHeaders[string(h.Name)] = string(h.Value)
	}
	method := string(req.Method)
	path := string(req.Path)
	version := string(req.Version)
	reqBody := string(req.Body)

	resp, err := b.p.ParseResponse(respBuf)
	if err != nil {
		return nil, err
	}
	respHeaders := make(map[string]string, len(resp.Headers))
	for _, h := range resp.Headers {
		respHeaders[string(h.Name)] = string(h.Value)
	}
	respBody := string(resp.Body)

	b.payload = Payload{
		Method:          method,
		Path:            path,
		Type:            version,
		StatusCode:      strconv.Itoa(resp.StatusCode),
		Status:          statusText(resp.StatusCode, resp.Reason),
		RequestHeaders:  reqHeaders,
		ResponseHeaders: respHeaders,
		RequestPayload:  reqBody,
		ResponsePayload: respBody,
	}
	return sonic.Marshal(&b.payload)
}

// BuildDirect (hand-rolled, HTTP keys only). The returned slice aliases the
// Builder's internal buffer and is only valid until the next Build*/BuildDirect
// call — copy it (e.g. string(out)) before reusing the Builder.
// Duplicate header names are preserved as duplicate JSON keys (lossless).
func (b *Builder) BuildDirect(reqBuf, respBuf []byte) ([]byte, error) {
	req, err := b.p.ParseRequest(reqBuf)
	if err != nil {
		return nil, err
	}
	resp, err := b.p.ParseResponse(respBuf)
	if err != nil {
		return nil, err
	}

	buf := b.scratch[:0]
	buf = append(buf, `{"method":`...)
	buf = appendJSONString(buf, req.Method)
	buf = kv(buf, "path", req.Path)
	buf = kv(buf, "type", req.Version)
	buf = kvStatusCode(buf, resp.StatusCode)
	buf = append(buf, `,"status":`...)
	buf = appendStatus(buf, resp.StatusCode, resp.Reason)
	buf = append(buf, `,"requestHeaders":`...)
	buf = appendHeaders(buf, req.Headers)
	buf = append(buf, `,"responseHeaders":`...)
	buf = appendHeaders(buf, resp.Headers)
	buf = kv(buf, "requestPayload", req.Body)
	buf = kv(buf, "responsePayload", resp.Body)
	buf = append(buf, '}')

	b.scratch = buf
	return buf, nil
}

// Meta holds the non-parsed fields (from TrafficContext + globals). Values are
// emitted as strings to match the legacy map[string]string data.
type Meta struct {
	SourceIP      string // -> "ip"
	DestIP        string // -> "destIp"
	TimeUnix      int64  // -> "time"
	AktoAccountID string // -> "akto_account_id"
	VxlanID       int    // -> "akto_vxlan_id"
	IsPending     bool   // -> "is_pending"
	Source        string // -> "source"
	Direction     int    // -> "direction"
	ProcessID     uint32 // -> "process_id"
	SocketID      uint32 // -> "socket_id"
	DaemonsetID   string // -> "daemonset_id"
	ProcessName   string // -> "process_name"
	EnableGraph   bool   // -> "enable_graph"
	Tag           string // -> "tag" (omitted if empty)
}

// BuildFull (PRODUCTION) — drop-in for parseHTTPTraffic + convertHeaders +
// buildJSONPayload + json.Marshal on the JSON/Kafka path. Emits every key,
// zero-alloc, one pass. Same aliasing/ownership rule as BuildDirect.
func (b *Builder) BuildFull(reqBuf, respBuf []byte, m *Meta) ([]byte, error) {
	req, err := b.p.ParseRequest(reqBuf)
	if err != nil {
		return nil, err
	}
	resp, err := b.p.ParseResponse(respBuf)
	if err != nil {
		return nil, err
	}

	buf := b.scratch[:0]
	buf = append(buf, `{"method":`...)
	buf = appendJSONString(buf, req.Method)
	buf = kv(buf, "path", req.Path)
	buf = kv(buf, "type", req.Version)
	buf = kvStatusCode(buf, resp.StatusCode)
	buf = append(buf, `,"status":`...)
	buf = appendStatus(buf, resp.StatusCode, resp.Reason)
	buf = append(buf, `,"requestHeaders":`...)
	buf = appendHeaders(buf, req.Headers)
	buf = append(buf, `,"responseHeaders":`...)
	buf = appendHeaders(buf, resp.Headers)
	buf = kv(buf, "requestPayload", req.Body)
	buf = kv(buf, "responsePayload", resp.Body)
	// metadata
	buf = kvStr(buf, "ip", m.SourceIP)
	buf = kvStr(buf, "destIp", m.DestIP)
	buf = kvInt(buf, "time", m.TimeUnix)
	buf = kvStr(buf, "akto_account_id", m.AktoAccountID)
	buf = kvInt(buf, "akto_vxlan_id", int64(m.VxlanID))
	buf = kvBool(buf, "is_pending", m.IsPending)
	buf = kvStr(buf, "source", m.Source)
	buf = kvInt(buf, "direction", int64(m.Direction))
	buf = kvInt(buf, "process_id", int64(m.ProcessID))
	buf = kvInt(buf, "socket_id", int64(m.SocketID))
	buf = kvStr(buf, "daemonset_id", m.DaemonsetID)
	buf = kvStr(buf, "process_name", m.ProcessName)
	buf = kvBool(buf, "enable_graph", m.EnableGraph)
	if m.Tag != "" {
		buf = kvStr(buf, "tag", m.Tag)
	}
	buf = append(buf, '}')

	b.scratch = buf
	return buf, nil
}

// ---- encoding helpers (all zero-alloc; leading comma assumed) ----

func kv(dst []byte, key string, val []byte) []byte {
	dst = appendKey(dst, key)
	return appendJSONString(dst, val)
}
func kvStr(dst []byte, key, val string) []byte {
	dst = appendKey(dst, key)
	dst = append(dst, '"')
	dst = appendJSONEscapedStr(dst, val)
	return append(dst, '"')
}
func kvInt(dst []byte, key string, val int64) []byte {
	dst = appendKey(dst, key)
	dst = append(dst, '"')
	dst = strconv.AppendInt(dst, val, 10)
	return append(dst, '"')
}
func kvBool(dst []byte, key string, val bool) []byte {
	dst = appendKey(dst, key)
	if val {
		return append(dst, `"true"`...)
	}
	return append(dst, `"false"`...)
}
func kvStatusCode(dst []byte, code int) []byte {
	dst = append(dst, `,"statusCode":"`...)
	dst = strconv.AppendInt(dst, int64(code), 10)
	return append(dst, '"')
}
func appendKey(dst []byte, key string) []byte {
	dst = append(dst, ',', '"')
	dst = append(dst, key...)
	return append(dst, '"', ':')
}
func appendStatus(dst []byte, code int, reason []byte) []byte {
	dst = append(dst, '"')
	dst = strconv.AppendInt(dst, int64(code), 10)
	if len(reason) > 0 {
		dst = append(dst, ' ')
		dst = appendJSONEscaped(dst, reason)
	}
	return append(dst, '"')
}

func appendHeaders(dst []byte, hs []httpparser.Header) []byte {
	dst = append(dst, '{')
	for i := range hs {
		if i > 0 {
			dst = append(dst, ',')
		}
		dst = appendJSONString(dst, hs[i].Name)
		dst = append(dst, ':')
		dst = appendJSONString(dst, hs[i].Value)
	}
	return append(dst, '}')
}

func appendJSONString(dst, s []byte) []byte {
	dst = append(dst, '"')
	dst = appendJSONEscaped(dst, s)
	return append(dst, '"')
}

const hexdigits = "0123456789abcdef"

// appendJSONEscaped writes s escaping ", \, control chars (<0x20), and replacing
// invalid UTF-8 bytes with U+FFFD (�) so the output is always valid JSON.
// Valid input (incl. valid multibyte UTF-8) takes the allocation-free fast path.
func appendJSONEscaped(dst, s []byte) []byte {
	start := 0
	i := 0
	for i < len(s) {
		c := s[i]
		if c < 0x80 {
			if c >= 0x20 && c != '"' && c != '\\' {
				i++
				continue
			}
			dst = append(dst, s[start:i]...)
			dst = appendEscByte(dst, c)
			i++
			start = i
			continue
		}
		r, size := utf8.DecodeRune(s[i:])
		if r == utf8.RuneError && size == 1 {
			dst = append(dst, s[start:i]...)
			dst = append(dst, '\\', 'u', 'f', 'f', 'f', 'd')
			i++
			start = i
			continue
		}
		i += size
	}
	return append(dst, s[start:]...)
}

// appendJSONEscapedStr is the string twin of appendJSONEscaped.
func appendJSONEscapedStr(dst []byte, s string) []byte {
	start := 0
	i := 0
	for i < len(s) {
		c := s[i]
		if c < 0x80 {
			if c >= 0x20 && c != '"' && c != '\\' {
				i++
				continue
			}
			dst = append(dst, s[start:i]...)
			dst = appendEscByte(dst, c)
			i++
			start = i
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 1 {
			dst = append(dst, s[start:i]...)
			dst = append(dst, '\\', 'u', 'f', 'f', 'f', 'd')
			i++
			start = i
			continue
		}
		i += size
	}
	return append(dst, s[start:]...)
}

func appendEscByte(dst []byte, c byte) []byte {
	switch c {
	case '"':
		return append(dst, '\\', '"')
	case '\\':
		return append(dst, '\\', '\\')
	case '\n':
		return append(dst, '\\', 'n')
	case '\r':
		return append(dst, '\\', 'r')
	case '\t':
		return append(dst, '\\', 't')
	default:
		return append(dst, '\\', 'u', '0', '0', hexdigits[c>>4], hexdigits[c&0xf])
	}
}
