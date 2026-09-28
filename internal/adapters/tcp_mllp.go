package adapters

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/weavster-dev/weavster/internal/codecs"
)

// MLLP framing delimiters (minimal lower-layer protocol, spec §8 TCP MLLP).
const mllpStart = 0x0B // VT

var mllpEnd = []byte{0x1C, 0x0D} // FS CR

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

// MLLPSinkTimeout bounds one delivery (connect, send, and the ACK) unless
// NewMLLPSinkWith sets another bound.
const MLLPSinkTimeout = 30 * time.Second

// maxACKBytes bounds the reply an MLLP sink reads.
const maxACKBytes = 1 << 20

// MLLPSink delivers HL7 v2 messages over TCP with MLLP framing and checks
// the receiver's ACK (#107 D-64). Each delivery uses its own connection, so
// a dropped connection never affects the next one.
type MLLPSink struct {
	addr    string
	timeout time.Duration
	dialer  func(ctx context.Context, addr string) (net.Conn, error)
	framing MLLPFraming
	noACK   bool
}

// NewMLLPSink returns an MLLP sink for addr with the default timeout.
func NewMLLPSink(addr string) *MLLPSink { return NewMLLPSinkWith(addr, 0) }

// NewMLLPSinkWith returns an MLLP sink for addr whose deliveries take at
// most timeout (MLLPSinkTimeout when not positive).
func NewMLLPSinkWith(addr string, timeout time.Duration) *MLLPSink {
	if timeout <= 0 {
		timeout = MLLPSinkTimeout
	}
	return &MLLPSink{addr: addr, timeout: timeout, dialer: func(ctx context.Context, addr string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "tcp", addr)
	}}
}

// NewMLLPSinkTLS returns an MLLP sink for addr that connects over TLS with
// cfg; deliveries take at most timeout, as for NewMLLPSinkWith.
func NewMLLPSinkTLS(addr string, timeout time.Duration, cfg *tls.Config) *MLLPSink {
	s := NewMLLPSinkWith(addr, timeout)
	s.dialer = func(ctx context.Context, addr string) (net.Conn, error) {
		d := tls.Dialer{Config: cfg}
		return d.DialContext(ctx, "tcp", addr) // includes the handshake
	}
	return s
}

// WithMode sets the sink's framing (zero: MLLP's) and whether it waits for
// an ACK; with noACK a message is delivered once it is written.
func (s *MLLPSink) WithMode(framing MLLPFraming, noACK bool) *MLLPSink {
	s.framing, s.noACK = framing, noACK
	return s
}

func (s *MLLPSink) Name() string { return "tcp" }

// Write sends m and waits for its ACK: AA or CA delivers it; any other code,
// a reply that is not an ACK, an ACK for another control id, or no reply
// in time is an error. Errors name the ACK code, never message content.
// Without ACKs (WithMode), m is delivered once it is written.
func (s *MLLPSink) Write(ctx context.Context, m Message) error {
	framing := s.framing.orDefault()
	if bytes.Contains(m.Body, framing.End) || bytes.IndexByte(m.Body, framing.Start) >= 0 {
		return fmt.Errorf("mllp: the message contains the frame's start byte (%02X) or end bytes (%X) and cannot be framed", framing.Start, framing.End)
	}
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	conn, err := s.dialer(ctx, s.addr)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	// A cancelled caller stops a blocked write or ACK read at once.
	stop := context.AfterFunc(ctx, func() { _ = conn.SetDeadline(time.Now()) })
	defer stop()
	if _, err := conn.Write(framing.wrap(m.Body)); err != nil {
		return err
	}
	if s.noACK {
		drain(ctx, conn)
		return nil
	}
	reply, err := readFramed(bufio.NewReader(conn), maxACKBytes, framing)
	if err != nil {
		if code := ErrorCode(err); code != "" {
			return withCode(code, fmt.Errorf("mllp: no ACK: %w", err))
		}
		return withCode("mllp:no-ack", fmt.Errorf("mllp: no ACK: %w", err))
	}
	code, acked, ok := codecs.ParseHL7ACK(reply)
	switch {
	case !ok:
		return withCode("mllp:not-an-ack", errors.New("mllp: the reply is not an HL7 ACK"))
	case code == codecs.AckApplicationError || code == codecs.AckCommitError:
		return withCode("mllp:"+code, fmt.Errorf("mllp: ACK %s (application error)", code))
	case code == codecs.AckApplicationReject || code == codecs.AckCommitReject:
		return withCode("mllp:"+code, fmt.Errorf("mllp: ACK %s (application reject)", code))
	case code != codecs.AckApplicationAccept && code != codecs.AckCommitAccept:
		return withCode("mllp:unknown-code", fmt.Errorf("mllp: ACK with an unknown code %q", code[:min(len(code), 8)])) // bounded: from the receiver
	case acked != codecs.HL7ControlID(m.Body):
		// An accept counts only for this message.
		return withCode("mllp:wrong-message", errors.New("mllp: the ACK is for another message (MSA-2 does not match MSH-10)"))
	}
	return nil
}

// noACKDrain bounds how long a sink without ACKs waits for the receiver
// to close after the message was sent.
const noACKDrain = time.Second

// drain ends the sending side of conn and reads what the receiver still
// sends for a moment (never past ctx's deadline, and not at all once ctx
// is done), so closing conn does not reset it (a reset can drop data not
// yet sent).
func drain(ctx context.Context, conn net.Conn) {
	if cw, ok := conn.(interface{ CloseWrite() error }); ok {
		_ = cw.CloseWrite()
	}
	deadline := time.Now().Add(noACKDrain)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	_ = conn.SetReadDeadline(deadline)
	if ctx.Err() != nil { // the cancel's deadline may have been replaced
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(conn, maxACKBytes))
}

func (s *MLLPSink) Close() error { return nil }

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
