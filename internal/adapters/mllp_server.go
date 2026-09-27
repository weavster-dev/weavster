package adapters

import (
	"bufio"
	"bytes"
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
		if conn.SetReadDeadline(time.Now().Add(s.opts.FrameTimeout)) != nil {
			return
		}
		frame, err := readFrame(r, s.opts.MaxFrame)
		if err != nil && !errors.Is(err, ErrMLLPFrameTooLarge) {
			return // closed, idle, or broken framing
		}
		reply := s.handle(frame, err)
		if conn.SetWriteDeadline(time.Now().Add(mllpWriteTimeout)) != nil {
			return
		}
		if _, err := conn.Write(frameMLLP(reply)); err != nil {
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
	for {
		b, err := r.ReadByte()
		if err != nil {
			return nil, err
		}
		if b == mllpStart {
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
		chunk, err := r.ReadSlice(mllpEnd[0])
		if errors.Is(err, bufio.ErrBufferFull) {
			add(chunk)
			continue
		}
		if err != nil {
			return nil, err
		}
		add(chunk[:len(chunk)-1])
		next, err := r.ReadByte()
		if err != nil {
			return nil, err
		}
		if next == mllpEnd[1] {
			if tooLarge {
				return frame, ErrMLLPFrameTooLarge
			}
			if frame == nil {
				frame = []byte{}
			}
			return frame, nil
		}
		// An FS inside the message: keep it and look at next again.
		add(mllpEnd[:1])
		_ = r.UnreadByte()
	}
}
