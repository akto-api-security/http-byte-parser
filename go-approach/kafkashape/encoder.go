package kafkashape

import "goapproach/httpparser"

// Encoder turns a parsed request/response pair + Meta into a single contiguous
// frame. The returned bytes are a view into the encoder's reused buffer and are
// valid only until the next Encode call on the same encoder. NOT concurrency-safe.
//
// Three implementations share this interface; switching wire formats is a single
// string: NewEncoder("json" | "flatbuffers" | "capnp").
type Encoder interface {
	Encode(req *httpparser.Request, resp *httpparser.Response, m *Meta) []byte
}

// *Builder (the hand-rolled JSON encoder) already satisfies Encoder.
var _ Encoder = (*Builder)(nil)

// Encoders is the registry of available wire formats.
var Encoders = map[string]func() Encoder{
	"json":        func() Encoder { return NewBuilder() },
	"flatbuffers": func() Encoder { return NewFBEncoder() },
	"capnp":       func() Encoder { return NewCapnpEncoder() },
}

// NewEncoder returns a fresh encoder for the named format, or nil if unknown.
func NewEncoder(kind string) Encoder {
	if f, ok := Encoders[kind]; ok {
		return f()
	}
	return nil
}
