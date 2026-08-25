package executor

import (
	"context"
	"errors"
	"testing"
)

// stubEngine is a minimal TransformEngine test double that records the last
// request it was called with and returns a configurable output/error.
type stubEngine struct {
	out     []byte
	err     error
	lastReq Request
	calls   int
}

func (s *stubEngine) Transform(ctx context.Context, req Request) ([]byte, error) {
	s.calls++
	s.lastReq = req
	return s.out, s.err
}

func TestNewResponseStage(t *testing.T) {
	e := &stubEngine{}
	rs := NewResponseStage(e)
	if rs == nil {
		t.Fatal("NewResponseStage returned nil")
	}
	if rs.engine != TransformEngine(e) {
		t.Errorf("engine = %v, want %v", rs.engine, e)
	}
}

func TestTransformResponse(t *testing.T) {
	e := &stubEngine{out: []byte("transformed")}
	rs := NewResponseStage(e)

	req := Request{ModuleName: "resp-mod", Version: "2", Input: []byte("in")}
	out, err := rs.TransformResponse(context.Background(), req)
	if err != nil {
		t.Fatalf("TransformResponse: %v", err)
	}
	if string(out) != "transformed" {
		t.Errorf("output = %q, want %q", out, "transformed")
	}
	if e.calls != 1 {
		t.Errorf("engine called %d times, want 1", e.calls)
	}
	if e.lastReq.ModuleName != "resp-mod" || e.lastReq.Version != "2" {
		t.Errorf("engine got req %+v, want ModuleName=resp-mod Version=2", e.lastReq)
	}
}

func TestTransformResponseError(t *testing.T) {
	wantErr := errors.New("boom")
	e := &stubEngine{err: wantErr}
	rs := NewResponseStage(e)

	out, err := rs.TransformResponse(context.Background(), Request{ModuleName: "resp-mod"})
	if !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want %v", err, wantErr)
	}
	if out != nil {
		t.Errorf("output = %v, want nil on error", out)
	}
}
