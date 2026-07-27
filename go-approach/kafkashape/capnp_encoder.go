package kafkashape

import (
	capnp "capnproto.org/go/capnp/v3"
	"goapproach/httpparser"
)

// CapnpEncoder builds a Cap'n Proto message over a reused single-segment backing
// buffer, so the message *data* isn't reallocated for bodies that fit. Note: this
// go-capnp version has no MarshalTo, so Message.Marshal allocates its output each
// call, and NewMessage allocates a small Message struct — unlike FlatBuffers this
// path is not fully allocation-free. See the bench for the exact allocs/op.
type CapnpEncoder struct {
	buf []byte // reused segment backing array
}

func NewCapnpEncoder() *CapnpEncoder {
	return &CapnpEncoder{buf: make([]byte, 0, 64*1024)}
}

func (e *CapnpEncoder) Encode(req *httpparser.Request, resp *httpparser.Response, m *Meta) []byte {
	msg, seg, err := capnp.NewMessage(capnp.SingleSegment(e.buf[:0]))
	if err != nil {
		return nil
	}
	root, err := NewRootCpHttpPair(seg)
	if err != nil {
		return nil
	}

	root.SetMethod(req.Method)
	root.SetPath(req.Path)
	root.SetVersion(req.Version)
	root.SetStatusCode(int32(resp.StatusCode))
	root.SetReason(resp.Reason)
	root.SetReqBody(req.Body)
	root.SetRespBody(resp.Body)

	if hl, err := root.NewReqHeaders(int32(len(req.Headers))); err == nil {
		for i := range req.Headers {
			h := hl.At(i)
			h.SetName(req.Headers[i].Name)
			h.SetValue(req.Headers[i].Value)
		}
	}
	if hl, err := root.NewRespHeaders(int32(len(resp.Headers))); err == nil {
		for i := range resp.Headers {
			h := hl.At(i)
			h.SetName(resp.Headers[i].Name)
			h.SetValue(resp.Headers[i].Value)
		}
	}

	root.SetSourceIp(m.SourceIP)
	root.SetDestIp(m.DestIP)
	root.SetAktoAccountId(m.AktoAccountID)
	root.SetSource(m.Source)
	root.SetDaemonsetId(m.DaemonsetID)
	root.SetProcessName(m.ProcessName)
	root.SetTag(m.Tag)
	root.SetTimeUnix(m.TimeUnix)
	root.SetVxlanId(int32(m.VxlanID))
	root.SetDirection(int32(m.Direction))
	root.SetProcessId(m.ProcessID)
	root.SetSocketId(m.SocketID)
	root.SetIsPending(m.IsPending)
	root.SetEnableGraph(m.EnableGraph)

	out, err := msg.Marshal()
	if err != nil {
		return nil
	}
	// Keep the (possibly grown) backing array for the next call.
	if d := seg.Data(); cap(d) > cap(e.buf) {
		e.buf = d[:0]
	}
	return out
}
