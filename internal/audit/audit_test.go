package audit

import (
	"context"
	"io"
	"log/slog"
	"testing"
)

func TestLocalSink(t *testing.T) {
	sink := NewLocalSink(slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := sink.Record(context.Background(), Entry{Actor: "admin", Action: ActionLogin, Resource: "console"}); err != nil {
		t.Fatal(err)
	}
	entries := sink.Entries()
	if len(entries) != 1 {
		t.Fatalf("entries = %d", len(entries))
	}
	if entries[0].Actor != "admin" || entries[0].Action != ActionLogin || entries[0].ID != 1 {
		t.Errorf("entry = %+v", entries[0])
	}
}

func TestNewLocalSinkUsesDefaultLogger(t *testing.T) {
	sink := NewLocalSink(nil)
	if sink.logger == nil {
		t.Fatal("logger is nil")
	}
	if err := sink.Record(context.Background(), Entry{Actor: "admin", Action: ActionLogin, Resource: "console"}); err != nil {
		t.Fatal(err)
	}
}

func TestPHIAccessRedaction(t *testing.T) {
	sink := NewLocalSink(slog.New(slog.NewTextHandler(io.Discard, nil)))
	err := RecordPHIAccess(context.Background(), sink, "alice", "message:42", map[string]string{
		"patient":  "12345",
		"password": "hunter2",
		"token":    "sekrit",
	})
	if err != nil {
		t.Fatal(err)
	}
	e := sink.Entries()[0]
	if e.Action != ActionPHIAccess {
		t.Errorf("action = %s", e.Action)
	}
	if e.Detail["password"] != "[redacted]" || e.Detail["token"] != "[redacted]" {
		t.Errorf("sensitive values not redacted: %+v", e.Detail)
	}
	if e.Detail["patient"] != "12345" {
		t.Errorf("non-sensitive value lost: %+v", e.Detail)
	}
}
