package adapters

import (
	"bytes"
	"context"
	"io"
	"net"
)

// MLLP framing delimiters (minimal lower-layer protocol, spec §8 TCP MLLP).
const mllpStart = 0x0B // VT

var mllpEnd = []byte{0x1C, 0x0D} // FS CR

// frameMLLP wraps body in an MLLP frame.
func frameMLLP(body []byte) []byte {
	out := make([]byte, 0, len(body)+3)
	out = append(out, mllpStart)
	out = append(out, body...)
	out = append(out, mllpEnd...)
	return out
}

// readMLLPFrame reads one MLLP frame from r (leading start byte through the
// FS CR terminator).
func readMLLPFrame(r io.Reader) ([]byte, error) {
	var buf bytes.Buffer
	// Expect the start byte.
	start := make([]byte, 1)
	if _, err := io.ReadFull(r, start); err != nil {
		return nil, err
	}
	if start[0] != mllpStart {
		return nil, io.ErrUnexpectedEOF
	}
	one := make([]byte, 1)
	for {
		if _, err := io.ReadFull(r, one); err != nil {
			return nil, err
		}
		if one[0] == mllpEnd[0] {
			if _, err := io.ReadFull(r, one); err != nil {
				return nil, err
			}
			if one[0] == mllpEnd[1] {
				return buf.Bytes(), nil
			}
			// Not a terminator: keep the bytes.
			buf.WriteByte(mllpEnd[0])
			buf.WriteByte(one[0])
			continue
		}
		buf.WriteByte(one[0])
	}
}

// MLLPSink delivers messages over TCP using MLLP framing.
type MLLPSink struct {
	addr   string
	conn   net.Conn
	dialer func(ctx context.Context, addr string) (net.Conn, error)
}

// NewMLLPSink returns a TCP MLLP sink for addr.
func NewMLLPSink(addr string) *MLLPSink {
	return &MLLPSink{addr: addr, dialer: func(ctx context.Context, addr string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "tcp", addr)
	}}
}

func (s *MLLPSink) Name() string { return "tcp" }

func (s *MLLPSink) Write(ctx context.Context, m Message) error {
	if s.conn == nil {
		conn, err := s.dialer(ctx, s.addr)
		if err != nil {
			return err
		}
		s.conn = conn
	}
	_, err := s.conn.Write(frameMLLP(m.Body))
	return err
}

func (s *MLLPSink) Close() error {
	if s.conn != nil {
		return s.conn.Close()
	}
	return nil
}

// MLLPSource accepts TCP connections and reads MLLP-framed messages.
type MLLPSource struct {
	listener net.Listener
	conn     net.Conn
}

// ListenMLLP binds an MLLP source on addr.
func ListenMLLP(addr string) (*MLLPSource, error) {
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	return &MLLPSource{listener: l}, nil
}

// Addr returns the bound address.
func (s *MLLPSource) Addr() net.Addr { return s.listener.Addr() }

func (s *MLLPSource) Name() string { return "tcp" }

// Read accepts a connection (lazily) and reads one MLLP frame.
func (s *MLLPSource) Read(ctx context.Context) (Message, error) {
	if s.conn == nil {
		conn, err := s.listener.Accept()
		if err != nil {
			return Message{}, err
		}
		s.conn = conn
	}
	body, err := readMLLPFrame(s.conn)
	if err != nil {
		return Message{}, err
	}
	return Message{Body: body}, nil
}

func (s *MLLPSource) Close() error {
	if s.conn != nil {
		_ = s.conn.Close()
	}
	return s.listener.Close()
}
