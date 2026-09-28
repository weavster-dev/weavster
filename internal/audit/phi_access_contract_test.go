package audit

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestRedactSensitivePreservesInput(t *testing.T) {
	for _, key := range []string{"password", "token", "secret", "authorization", "credential", "ssn", "phi"} {
		t.Run(key, func(t *testing.T) {
			input := map[string]string{key: "sensitive-value", "message_id": "42"}
			got := RedactSensitive(input)
			want := map[string]string{key: "[redacted]", "message_id": "42"}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("redacted detail = %v, want %v", got, want)
			}
			if input[key] != "sensitive-value" {
				t.Fatal("redaction modified the caller's sensitive value")
			}
			got["message_id"] = "changed"
			if input["message_id"] != "42" {
				t.Fatal("returned detail aliases the input map")
			}
		})
	}
}

type phiCaptureSink struct {
	ctx   context.Context
	entry Entry
	calls int
	err   error
}

func (s *phiCaptureSink) Record(ctx context.Context, entry Entry) error {
	s.ctx, s.entry = ctx, entry
	s.calls++
	return s.err
}

func TestRecordPHIAccessSinkContract(t *testing.T) {
	sinkErr := errors.New("audit sink unavailable")
	for _, tc := range []struct {
		name string
		err  error
	}{
		{name: "success"},
		{name: "sink failure", err: sinkErr},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			sink := &phiCaptureSink{err: tc.err}
			detail := map[string]string{"ssn": "sensitive-value", "message_id": "42"}
			if err := RecordPHIAccess(ctx, sink, "alice", "message:42", detail); !errors.Is(err, tc.err) {
				t.Fatalf("error = %v, want %v", err, tc.err)
			}
			if sink.calls != 1 || sink.ctx != ctx {
				t.Fatalf("sink calls = %d, context forwarded = %v", sink.calls, sink.ctx == ctx)
			}
			if sink.entry.Actor != "alice" || sink.entry.Resource != "message:42" || sink.entry.Action != ActionPHIAccess {
				t.Fatalf("entry identity = %+v", sink.entry)
			}
			want := map[string]string{"ssn": "[redacted]", "message_id": "42"}
			if !reflect.DeepEqual(sink.entry.Detail, want) {
				t.Fatalf("sink detail = %v, want %v", sink.entry.Detail, want)
			}
			if detail["ssn"] != "sensitive-value" {
				t.Fatal("recording modified the caller's detail")
			}
		})
	}
}
