package adapters

import (
	"context"
	"io"
)

// Interflow is an in-memory pipe routing messages between flows in-process
// (spec §2.3.9, §8 in-memory inter-flow).
type Interflow struct {
	ch chan Message
}

// NewInterflow returns an in-memory inter-flow pipe with the given buffer.
func NewInterflow(buffer int) *Interflow {
	return &Interflow{ch: make(chan Message, buffer)}
}

// Source returns the reading end.
func (f *Interflow) Source() Source { return &interflowSource{f} }

// Sink returns the writing end.
func (f *Interflow) Sink() Sink { return &interflowSink{f} }

type interflowSource struct{ f *Interflow }

func (s *interflowSource) Name() string { return "in-memory" }

func (s *interflowSource) Read(ctx context.Context) (Message, error) {
	select {
	case m, ok := <-s.f.ch:
		if !ok {
			return Message{}, io.EOF
		}
		return m, nil
	case <-ctx.Done():
		return Message{}, ctx.Err()
	}
}

func (s *interflowSource) Close() error { return nil }

type interflowSink struct{ f *Interflow }

func (s *interflowSink) Name() string { return "in-memory" }

func (s *interflowSink) Write(ctx context.Context, m Message) error {
	select {
	case s.f.ch <- m:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *interflowSink) Close() error {
	close(s.f.ch)
	return nil
}
