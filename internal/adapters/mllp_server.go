package adapters

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"net"
	"sync"
	"time"
)

// ErrMLLPFrameTooLarge reports a frame over the server's limit; the frame
// was read to its end and discarded (only its first segment is returned,
// so the reply can name the message), and the connection can go on.
var ErrMLLPFrameTooLarge = errors.New("mllp: frame too large")

// MLLPHandler answers one received frame with the reply to send back (an
// HL7 ACK). err is ErrMLLPFrameTooLarge when the frame was discarded.
type MLLPHandler func(frame []byte, err error) []byte

// MLLPOptions bound an MLLP server's connections.
type MLLPOptions struct {
	MaxFrame    int           // largest frame kept, in bytes
	IdleTimeout time.Duration // a connection that sends nothing for this long is closed
	// FrameTimeout bounds receiving one frame once its first byte arrived,
	// so a slow sender of a large message is not cut off by IdleTimeout.
	FrameTimeout time.Duration
	// HandshakeTimeout bounds a TLS connection's handshake (the listener
	// is a tls listener), so a stalled client does not wait IdleTimeout.
	HandshakeTimeout time.Duration
	// Framing delimits frames (zero: MLLP's VT … FS CR).
	Framing MLLPFraming
	// NoReply sends nothing back: the handler's reply is discarded.
	NoReply bool
}

// MLLPFraming is the bytes around each frame: Start, then the message, then
// End (one or two bytes). The zero value is MLLP's VT … FS CR.
type MLLPFraming struct {
	Start byte
	End   []byte
}

// orDefault is f, or MLLP's framing when f is the zero value.
func (f MLLPFraming) orDefault() MLLPFraming {
	if len(f.End) == 0 {
		return MLLPFraming{Start: mllpStart, End: mllpEnd}
	}
	return f
}

// wrap frames body.
func (f MLLPFraming) wrap(body []byte) []byte {
	f = f.orDefault()
	out := make([]byte, 0, len(body)+1+len(f.End))
	out = append(out, f.Start)
	out = append(out, body...)
	return append(out, f.End...)
}

// maxFrameHead is how much of an oversize frame's first segment is kept.
const maxFrameHead = 4096

// mllpWriteTimeout bounds sending one reply.
const mllpWriteTimeout = 30 * time.Second

// MLLPServer accepts TCP connections and answers every MLLP frame on them
// with the handler's reply. Connections are served concurrently; frames on
// one connection one after another, in order.
type MLLPServer struct {
	ln      net.Listener
	handle  MLLPHandler
	opts    MLLPOptions
	done    chan struct{} // closed when the accept loop ends
	conns   sync.WaitGroup
	mu      sync.Mutex
	open    map[net.Conn]struct{}
	closing bool
}

// ServeMLLP serves ln until Close.
func ServeMLLP(ln net.Listener, handle MLLPHandler, opts MLLPOptions) *MLLPServer {
	s := &MLLPServer{ln: ln, handle: handle, opts: opts, done: make(chan struct{}), open: map[net.Conn]struct{}{}}
	go s.accept()
	return s
}

// Done is closed when the server stops accepting connections.
func (s *MLLPServer) Done() <-chan struct{} { return s.done }

func (s *MLLPServer) accept() {
	defer close(s.done)
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() {
				continue
			}
			return
		}
		s.mu.Lock()
		if s.closing {
			s.mu.Unlock()
			_ = conn.Close()
			return
		}
		s.open[conn] = struct{}{}
		s.conns.Add(1)
		s.mu.Unlock()
		go s.serve(conn)
	}
}

// serve answers the frames on conn until it closes, idles out, or the
// server closes; a frame being handled is always finished and answered.
func (s *MLLPServer) serve(conn net.Conn) {
	defer s.conns.Done()
	defer func() {
		s.mu.Lock()
		delete(s.open, conn)
		s.mu.Unlock()
		_ = conn.Close()
	}()
	if tc, ok := conn.(*tls.Conn); ok {
		ctx, cancel := context.WithTimeout(context.Background(), s.opts.HandshakeTimeout)
		err := tc.HandshakeContext(ctx)
		cancel()
		if err != nil {
			return
		}
	}
	r := bufio.NewReader(conn)
	for {
		// Under the lock, so Close either sees this deadline set and cuts
		// it short, or this loop sees closing.
		s.mu.Lock()
		stop := s.closing || conn.SetReadDeadline(time.Now().Add(s.opts.IdleTimeout)) != nil
		s.mu.Unlock()
		if stop {
			return
		}
		if _, err := r.Peek(1); err != nil { // waits, at most IdleTimeout, for the next frame
			return
		}
		// Also under the lock: Close must be able to cut a frame short.
		s.mu.Lock()
		stop = s.closing || conn.SetReadDeadline(time.Now().Add(s.opts.FrameTimeout)) != nil
		s.mu.Unlock()
		if stop {
			return
		}
		frame, err := readFramed(r, s.opts.MaxFrame, s.opts.Framing)
		if err != nil && !errors.Is(err, ErrMLLPFrameTooLarge) {
			return // closed, idle, or broken framing
		}
		reply := s.handle(frame, err)
		if s.opts.NoReply {
			continue
		}
		if conn.SetWriteDeadline(time.Now().Add(mllpWriteTimeout)) != nil {
			return
		}
		if _, err := conn.Write(s.opts.Framing.wrap(reply)); err != nil {
			return
		}
	}
}

// Close stops accepting, ends idle connections at once, lets frames being
// handled finish and be answered, and returns when every connection is
// closed. A frame only partly received is dropped unanswered.
func (s *MLLPServer) Close() error {
	s.mu.Lock()
	s.closing = true
	for conn := range s.open {
		_ = conn.SetReadDeadline(time.Now()) // unblocks a waiting read
	}
	s.mu.Unlock()
	err := s.ln.Close()
	<-s.done
	s.conns.Wait()
	return err
}

// readFrame reads one MLLP frame (VT … FS CR) from r, skipping bytes before
// the start byte (such as line breaks between frames). A frame over max is
// read to its end and reported as ErrMLLPFrameTooLarge, with its first
// segment (up to maxFrameHead bytes).
func readFrame(r *bufio.Reader, max int) ([]byte, error) {
	return readFramed(r, max, MLLPFraming{})
}

// readFramed is readFrame with framing f.
func readFramed(r *bufio.Reader, max int, f MLLPFraming) ([]byte, error) {
	f = f.orDefault()
	for {
		b, err := r.ReadByte()
		if err != nil {
			return nil, err
		}
		if b == f.Start {
			break
		}
	}
	var frame []byte
	tooLarge := false
	add := func(p []byte) {
		if !tooLarge && len(frame)+len(p) > max {
			tooLarge = true
			frame = append(frame, p...)
			if i := bytes.IndexAny(frame, "\r\n"); i >= 0 {
				frame = frame[:i]
			}
			frame = frame[:min(len(frame), maxFrameHead):min(len(frame), maxFrameHead)]
		}
		if !tooLarge {
			frame = append(frame, p...)
		}
	}
	for {
		chunk, err := r.ReadSlice(f.End[0])
		if errors.Is(err, bufio.ErrBufferFull) {
			add(chunk)
			continue
		}
		if err != nil {
			return nil, err
		}
		add(chunk[:len(chunk)-1])
		if len(f.End) == 1 {
			return frameDone(frame, tooLarge)
		}
		next, err := r.ReadByte()
		if err != nil {
			return nil, err
		}
		if next == f.End[1] {
			return frameDone(frame, tooLarge)
		}
		// End's first byte inside the message: keep it and look at next again.
		add(f.End[:1])
		_ = r.UnreadByte()
	}
}

// frameDone is a complete frame: ErrMLLPFrameTooLarge with its head when it was
// over the limit, else never nil (an empty frame is an empty message).
func frameDone(frame []byte, tooLarge bool) ([]byte, error) {
	if tooLarge {
		return frame, ErrMLLPFrameTooLarge
	}
	if frame == nil {
		frame = []byte{}
	}
	return frame, nil
}
